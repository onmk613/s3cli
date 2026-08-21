// object-copy.go 实现同 endpoint 内的对象复制 CopyObjects (cp, 支持:
// --recursive/-r / --storage-class/--sc / --tags / --metadata),
// 单文件直传与目录递归 (走 RunStream), 目标 key 解析复用 s3path 规则.

package action

import (
	"context"
	"errors"
	"fmt"
	"path"
	"s3cli/internal/s3path"
	"strings"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/i18n"
	"s3cli/pkg/s3iface"
)

// CopyOptions cp/mv 命令参数.
type CopyOptions struct {
	Recursive    bool              // -r: 递归复制目录
	NoProgress   bool              // 不显示进度条（--quiet）
	StorageClass string            // --storage-class/--sc: 目标存储级别
	Tags         string            // --tags: 目标对象标签 'k1=v1&k2=v2'
	Metadata     map[string]string // --metadata: 目标对象自定义元数据 (需 REPLACE)
}

// CopyObjects 处理同对象存储之内的复制
func (c *Action) CopyObjects(opt CopyOptions, srcBucket, srcKey, destBucket, destKey string) error {
	srcTrailing := strings.HasSuffix(srcKey, "/")
	destTrailing := strings.HasSuffix(destKey, "/")

	// 源 key 是文件还是目录
	srcIsFile, err := c.IsS3File(srcBucket, srcKey)
	if err != nil {
		return fmt.Errorf("check source: %s", FormatAPIError(err))
	}
	// 为目录但是没有设置 -r
	if !srcIsFile && !opt.Recursive {
		return errors.New(i18n.T("source is a directory; use -r/--recursive", "源是目录；请使用 -r/--recursive"))
	}

	// 单文件源
	if srcIsFile {
		dst := s3path.ResolveFileDest(destKey, destTrailing, path.Base(strings.TrimSuffix(srcKey, "/")))
		if err := c.copyObject(opt, srcBucket, srcKey, destBucket, dst); err != nil {
			return err
		}
		myprint.PrintfGreen(i18n.T("cp: %s -> %s\n", "复制：%s -> %s\n"), c.S3Path(srcBucket, srcKey), c.S3Path(destBucket, dst))
		return nil
	}

	// 目录源
	state, err := c.DestStateOf(destBucket, destKey)
	if err != nil {
		myprint.PrintfYellow("check destination (treated as not-exist): %s\n", FormatAPIError(err))
		state = s3path.DestNone
	}
	if state == s3path.DestFile {
		return fmt.Errorf(i18n.T("%s: destination exists and is a file object; cannot copy a directory onto it",
			"%s：目标已存在且是文件对象；目录无法复制到单个对象上"), c.S3Path(destBucket, destKey))
	}
	destPrefix, appendRel := s3path.ResolveDirDestPrefix(srcKey, srcTrailing, destKey, destTrailing, state)
	if !appendRel {
		// appendRel=false 时目录下所有对象都会写到 destPrefix 自身, N 个对象互相
		// 覆盖只留最后一个 —— 静默数据丢失, 必须拒绝 (目标加 "/" 或指向已存在目录)。
		return fmt.Errorf(i18n.T("%s: target of a directory copy must be an existing directory or end with '/'",
			"%s：目录复制的目标必须是已存在的目录或以 '/' 结尾"), c.S3Path(destBucket, destKey))
	}
	if err := checkDirPrefixOverlap(srcBucket, srcKey, destBucket, destPrefix); err != nil {
		return err
	}
	return c.copyDirStreaming(opt, srcBucket, srcKey, destBucket, destPrefix, appendRel)
}

// checkDirPrefixOverlap 禁止同桶上源/目标前缀互相包含的目录复制/移动:
// 流式列举源前缀的同时向其内部写入新 key, 新 key 字典序更靠后, 会被后续分页
// 再次列出 —— cp 会级联复制到更深层, mv 会不断向更深处搬运, 二者都无法终止。
func checkDirPrefixOverlap(srcBucket, srcKey, destBucket, destPrefix string) error {
	if srcBucket != destBucket {
		return nil
	}
	src := normalizeMirrorPrefix(strings.Trim(srcKey, "/"))
	tgt := normalizeMirrorPrefix(strings.Trim(destPrefix, "/"))
	// 目标为桶根且源非空: 相对路径展开到根, 写入落在源前缀之外, 不会级联, 放行;
	// 其余同桶情形 (整桶为源 / 前缀互相包含 / 完全相同) 都会在列举进行中写入源前缀内部。
	overlap := src == tgt || (src == "" && tgt != "") ||
		(src != "" && tgt != "" && (strings.HasPrefix(src, tgt) || strings.HasPrefix(tgt, src)))
	if overlap {
		return fmt.Errorf(i18n.T(
			"source and target prefixes overlap on the same bucket (%q vs %q); copy/move to a sibling prefix instead",
			"同一存储桶上源与目标前缀重叠（%q 与 %q）；请复制/移动到兄弟前缀"),
			src, tgt)
	}
	return nil
}

// copyObject 单对象复制, 透传存储级别/标签/元数据参数.
func (c *Action) copyObject(opt CopyOptions, srcBucket, srcKey, destBucket, destKey string) error {
	copyOpts := &s3iface.CopyObjectOptions{
		StorageClass: opt.StorageClass,
	}
	if opt.Tags != "" {
		copyOpts.Tagging = opt.Tags
		copyOpts.TaggingDirective = "REPLACE"
	}
	if len(opt.Metadata) > 0 {
		copyOpts.Metadata = opt.Metadata
		copyOpts.MetadataDirective = "REPLACE"
	}
	_, err := c.S3.CopyObject(c.Ctx, srcBucket, srcKey, destBucket, destKey, copyOpts)
	if err != nil {
		return fmt.Errorf("copy: %s", FormatAPIError(err))
	}
	return nil
}

// copyDirStreaming 流式列出并并发复制，带进度条。
// destPrefix 为目标前缀；appendRel=true 时把每个源对象相对源前缀的路径拼到 destPrefix 之下，
// 否则所有源对象都写到 destPrefix（与规则 1/3 的 trailing-none/file 语义一致）。
func (c *Action) copyDirStreaming(opt CopyOptions, srcBucket, srcKey, destBucket, destPrefix string, appendRel bool) error {
	return RunStream(c.Ctx, StreamConfig{
		Concurrency: defaultConcurrency,
		Label:       "cp",
		NoProgress:  opt.NoProgress,
		Count: func(ctx context.Context, add func(n, size int64)) error {
			// 预统计同样跳过目录占位对象, 与 Scan/Count 的计数口径一致。
			return c.countS3Prefix(ctx, srcBucket, srcKey, true, add)
		},
		Scan: func(ctx context.Context, jobs chan<- StreamJob) error {
			return c.forEachObject(ctx, srcBucket, srcKey, func(obj s3iface.ObjectInfo) error {
				// 跳过 0 字节的目录占位对象 ("dir/" 形态), 与 get 的扫描一致:
				// 这类对象没有内容可复制, 复制过去只会留下无意义的目录标记。
				if strings.HasSuffix(obj.Key, "/") && obj.Size == 0 {
					return nil
				}
				dst := buildDestKey(obj.Key, srcKey, destPrefix, appendRel)
				jobs <- StreamJob{
					Src:  obj.Key,
					Dst:  c.S3Path(destBucket, dst),
					Size: obj.Size,
				}
				return nil
			})
		},
		Work: func(ctx context.Context, job StreamJob, _ func(n int64)) error {
			dstKey := buildDestKey(job.Src, srcKey, destPrefix, appendRel)
			return c.copyObject(opt, srcBucket, job.Src, destBucket, dstKey)
		},
	})
}

// buildDestKey 计算目录复制时单个源对象的目标 key。
//
//	srcKey      源对象绝对 key
//	srcPrefix   源前缀（可带尾斜杠）
//	destPrefix  目标前缀（不含尾斜杠）
//	appendRel   是否把源对象相对源前缀的路径拼到目标前缀下
//
// appendRel=false 时所有对象都写到 destPrefix 自身。
func buildDestKey(srcKey, srcPrefix, destPrefix string, appendRel bool) string {
	srcKey = strings.TrimPrefix(srcKey, "/")
	srcPrefix = strings.Trim(srcPrefix, "/")
	destPrefix = strings.Trim(destPrefix, "/")

	if !appendRel {
		return destPrefix
	}

	rel := srcKey
	if srcPrefix != "" && strings.HasPrefix(srcKey, srcPrefix) {
		// 去掉前缀及其后可选的斜杠, 等价于原正则 "^srcPrefix/?", 但避免热路径重复编译正则.
		rel = strings.TrimPrefix(strings.TrimPrefix(srcKey, srcPrefix), "/")
	}
	if rel == "" {
		return destPrefix
	}
	return path.Join(destPrefix, rel)
}
