// mirror-stream.go 提供 mirror (双端镜像同步) 所需的流式列举、归并差异、
// 过滤与 key 重映射原语. 这些函数以 channel 串联, 内存占用恒定, 不缓存全集.

package action

import (
	"fmt"
	"path"
	"strings"
	"time"

	"s3cli/pkg/s3iface"
)

// =============== 对象信息 ===============

// ObjectInfo 描述 mirror 列举阶段的单个对象 (相对源前缀的相对路径 + 元数据).
// 与 s3iface.ObjectInfo 不同, 此处的 Key 已剥离源前缀, 便于源/目标按相对路径归并.
type ObjectInfo struct {
	Key          string
	Size         int64
	ETag         string
	LastModified time.Time
}

// streamObjects 流式列出 bucket 下 prefix 的所有对象, 把每个对象 (相对路径)
// 按 S3 返回的字典序写入 out. S3 ListObjectsV2 保证 key 字典序递增; 去掉
// 固定前缀后, 相对 key 的相对顺序不变, 因此 out 也是有序流.
//
// 该函数不在内存里缓存全集 —— 列举与下游消费同时进行, 内存恒定.
// 出错时把错误写入 errCh 并提前关闭 out.
func streamObjects(c *S3Client, bucket, prefix string, out chan<- ObjectInfo, errCh chan<- error) {
	defer close(out)

	paginator := c.S3.NewListObjectsV2Paginator(bucket, &s3iface.ListObjectsV2Options{Prefix: prefix})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(c.Ctx)
		if err != nil {
			select {
			case errCh <- fmt.Errorf("list %s: %s", c.S3Path(bucket, prefix), FormatAPIError(err)):
			default:
			}
			return
		}
		for _, obj := range page.Contents {
			key := obj.Key
			info := ObjectInfo{
				Key:          relKey(key, prefix),
				Size:         obj.Size,
				ETag:         strings.Trim(obj.ETag, `"`),
				LastModified: obj.LastModified,
			}
			select {
			case out <- info:
			case <-c.Ctx.Done():
				select {
				case errCh <- c.Ctx.Err():
				default:
				}
				return
			}
		}
	}
}

// =============== 差异计算 (流式归并) ===============

// diffAction 描述归并产出的单个差异决策.
type diffAction struct {
	rel    string // 相对路径
	delete bool   // true=目标多余需删除; false=需 src->tgt 复制
	size   int64  // 复制时为源对象大小 (用于进度统计)
}

// streamDiff 归并两个 "有序" 对象流, 边读边产出差异决策到 actions.
//
// 因为两个流都按相对 key 字典序递增, 可做经典 merge-join:
//   - src 当前 key < tgt 当前 key  -> src 独有 -> COPY
//   - src 当前 key > tgt 当前 key  -> tgt 独有 -> DELETE
//   - 相等                          -> 两端都有, overwrite 且有变更才 COPY
//
// 内存占用 O(1) (仅各持有一个待比较对象), 不依赖全集装载.
func streamDiff(srcCh, tgtCh <-chan ObjectInfo, overwrite bool, actions chan<- diffAction) {
	defer close(actions)

	src, srcOK := <-srcCh
	tgt, tgtOK := <-tgtCh

	for srcOK && tgtOK {
		switch {
		case src.Key < tgt.Key:
			actions <- diffAction{rel: src.Key, size: src.Size}
			src, srcOK = <-srcCh
		case src.Key > tgt.Key:
			actions <- diffAction{rel: tgt.Key, delete: true}
			tgt, tgtOK = <-tgtCh
		default: // 相等
			if overwrite && needsUpdate(src, tgt) {
				actions <- diffAction{rel: src.Key, size: src.Size}
			}
			src, srcOK = <-srcCh
			tgt, tgtOK = <-tgtCh
		}
	}
	for srcOK { // 剩余源对象: 目标都没有 -> COPY
		actions <- diffAction{rel: src.Key, size: src.Size}
		src, srcOK = <-srcCh
	}
	for tgtOK { // 剩余目标对象: 源都没有 -> DELETE
		actions <- diffAction{rel: tgt.Key, delete: true}
		tgt, tgtOK = <-tgtCh
	}
}

// =============== 过滤 ===============

// matchesMirrorFilters 按约定的 include/exclude glob 判定相对 key 是否保留.
// exclude 命中即丢弃; include 为空表示全收, 否则需命中任一 include.
func matchesMirrorFilters(key string, include, exclude []string) bool {
	for _, pattern := range exclude {
		if matchMirrorGlob(pattern, key) {
			return false
		}
	}
	if len(include) == 0 {
		return true
	}
	for _, pattern := range include {
		if matchMirrorGlob(pattern, key) {
			return true
		}
	}
	return false
}

// matchMirrorGlob 用 path.Match 匹配; pattern 不含 "/" 时也匹配 basename,
// 便于用 "*.tmp" 这样的简单后缀过滤深层路径下的对象.
func matchMirrorGlob(pattern, key string) bool {
	if ok, _ := path.Match(pattern, key); ok {
		return true
	}
	if !strings.Contains(pattern, "/") {
		ok, _ := path.Match(pattern, path.Base(key))
		return ok
	}
	return false
}

// filterObjects 把 in 通道中通过 include/exclude 过滤的对象转发到返回通道.
func filterObjects(in <-chan ObjectInfo, include, exclude []string) <-chan ObjectInfo {
	out := make(chan ObjectInfo, 1024)
	go func() {
		defer close(out)
		for obj := range in {
			if matchesMirrorFilters(obj.Key, include, exclude) {
				out <- obj
			}
		}
	}()
	return out
}

// needsUpdate 判断源对象相对目标是否需要 (重新) 复制.
//
// 优先比 ETag (已去引号). MPU 上传时 ETag 形如 "xxx-N", 两端不可比,
// 此时退化到 size + last-modified.
func needsUpdate(src, tgt ObjectInfo) bool {
	if !strings.Contains(src.ETag, "-") && !strings.Contains(tgt.ETag, "-") &&
		src.ETag != "" && tgt.ETag != "" {
		return src.ETag != tgt.ETag
	}
	if src.Size != tgt.Size {
		return true
	}
	return src.LastModified.After(tgt.LastModified)
}

// =============== key 重映射 ===============

// relKey 把绝对 key 转为相对源前缀的路径.
//
//	prefix="a/b/", key="a/b/c/d.txt" -> "c/d.txt"
//	prefix="",     key="x/y.txt"     -> "x/y.txt"
//
// 调用前 prefix 已规范化为空或以 "/" 结尾 (见 normalizeMirrorPrefix),
// 因此列出结果的 key 必然以 prefix 开头, 直接 TrimPrefix 即可.
func relKey(key, prefix string) string {
	return strings.TrimPrefix(key, prefix)
}

// normalizeMirrorPrefix 把非空前缀规范化为以 "/" 结尾.
// 裸前缀 (如 "dir") 做 ListObjectsV2 会前缀碰撞 (误匹配 "dir2/x"、"dir-old/y"),
// 目标端同理会在 --remove 时误删前缀碰撞的对象, 因此源/目标列举前缀都必须规范化.
func normalizeMirrorPrefix(prefix string) string {
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		return prefix + "/"
	}
	return prefix
}

// joinKey 把相对路径拼到目标前缀下.
func joinKey(prefix, rel string) string {
	switch {
	case prefix == "":
		return rel
	case strings.HasSuffix(prefix, "/"):
		return prefix + rel
	default:
		return path.Join(prefix, rel)
	}
}
