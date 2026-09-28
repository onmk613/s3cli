// common.go 定义 action 包的核心: Action 类型、S3 路径格式化与存在性/目录探测
// (S3Path / IsS3File / DestStateOf), 以及对象遍历器 forEachObject.
// 取消判断、校验和算法解析与 MIME 注册等通用工具在 utils.go.

package action

import (
	"context"
	"errors"
	"fmt"
	"s3cli/internal/action/render"
	"s3cli/internal/api"
	"s3cli/internal/i18n"
	"s3cli/internal/s3path"
	"strings"
	"time"
)

// defaultConcurrency 是流式传输（get/put/cp/mv）与 mirror/diff 的默认并发数。
// 与 config.DefaultConcurrency 保持一致；action 不依赖 config 以维持与配置层解耦。
const defaultConcurrency = 10

// Action 封装 S3 操作后端, 持有 alias 和 ctx.
// S3 字段为 api.S3Operations 接口, 底层实现为自建请求的 api.Client
// (由 client 包统一构造).
type Action struct {
	S3    api.S3Operations
	Alias string
	Ctx   context.Context
}

// S3Path 格式化路径为 "alias:bucket/key", 和命令行格式一样。
// 实现在 render 包, 与其它展示格式集中在一处。
func (c *Action) S3Path(bucket, key string) string {
	return render.S3Path(c.Alias, bucket, key)
}

// S3PathStatic 静态版本, 无需 Action 实例, 用于无客户端上下文的格式化场景。
func S3PathStatic(alias, bucket, key string) string {
	return render.S3Path(alias, bucket, key)
}

// IsS3File 检查路径是文件 (true) 还是目录 / 不存在 (false)
func (c *Action) IsS3File(bucket, key string) (bool, error) {
	// 空 key 表示目标是 bucket 本身，不可能是文件
	if key == "" {
		return false, nil
	}

	_, err := c.S3.HeadObject(c.Ctx, bucket, key, "")
	if err == nil {
		return true, nil
	}

	// 按 ErrorResponse 的 Code 判断
	if apiErr, ok := errors.AsType[*api.ErrorResponse](err); ok {
		switch apiErr.Code {
		case "NoSuchKey", "NotFound", "404":
			// 对象不存在：可能是目录前缀，继续探测。
			return c.checkIfDirectory(bucket, key)
		case "AccessDenied", "Forbidden", "403":
			return false, fmt.Errorf(i18n.T("access denied to bucket '%s'", "拒绝访问存储桶 '%s'"), bucket)
		}
	}
	return false, fmt.Errorf("s3 error: %w", err)
}

// objectExists 仅判断对象是否存在 (true=存在), 不做目录前缀探测。
// 用于上传前的存在性检查: 目标要么是文件对象, 要么不存在。
// 与 IsS3File 不同, 404 直接判 false, 不再探测是否为目录前缀;
// 403 等权限错误以 error 返回 (无法确认存在性时宁可报错, 不静默上传)。
func (c *Action) objectExists(ctx context.Context, bucket, key string) (bool, error) {
	_, err := c.S3.HeadObject(ctx, bucket, key, "")
	if err == nil {
		return true, nil
	}
	if apiErr, ok := errors.AsType[*api.ErrorResponse](err); ok {
		switch apiErr.Code {
		case "NoSuchKey", "NotFound", "404":
			return false, nil
		}
	}
	return false, err
}

// checkIfDirectory 在 HeadObject 返回 404 后判断 key 是否为目录前缀。
// 返回 (false, nil) 表示是目录前缀（非文件），(false, err) 表示路径不存在。
//
// 目录判定只接受 "key + /" 开头的前缀探测:
//   - key 先去掉尾部 "/", 避免 key="dir/" 时拼出 "dir//" 的错误前缀
//     (与 DestStateOf 的 probe 处理一致);
//   - 曾有一层"裸 key 前缀"兜底 (Prefix=key, MaxKeys=1), 会把同名的
//     其他对象误判为目录 —— 例如 key="report" 而实际只存在 "report-2023.pdf"
//     时, 裸前缀探测命中 "report-2023.pdf", 于是把不存在的 "report" 当成目录。
//     该兜底与 key+"/" 探测等价 (命中裸前缀的 key 若不匹配 "key/..." 则既非
//     目录也非 key 本身), 因此整体移除, 只保留 key+"/" 探测。
func (c *Action) checkIfDirectory(bucket, key string) (bool, error) {
	probe := strings.TrimSuffix(key, "/")
	listResp, err := c.S3.ListObjectsV2(c.Ctx, bucket, &api.ListObjectsV2Options{
		Prefix:    probe + "/",
		Delimiter: "/",
		MaxKeys:   1,
	})
	if err != nil {
		return false, err
	}
	if len(listResp.CommonPrefixes) > 0 || len(listResp.Contents) > 0 {
		return false, nil
	}
	// 语义上就是 404: 用 api.NewNotFound 构造, 让上层 IsNotFound / 退出码映射
	// 能识别。否则 `get`/`cp` 一个不存在的对象会退化成退出码 1, 与 README 承诺的
	// "4 = 对象/桶不存在" 不符 —— 路径判定是本地逻辑, 错误链上本来没有服务端响应。
	return false, api.NewNotFound(fmt.Sprintf(
		i18n.T("path '%s' does not exist in bucket '%s'", "路径 '%s' 在存储桶 '%s' 中不存在"), key, bucket))
}

// DestStateOf 判断目标 key 当前的状态：文件 / 目录 / 不存在。
// 用于 cp/mv/mirror 计算目标对象 key。探测失败时返回 (DestNone, err)。
func (c *Action) DestStateOf(bucket, key string) (s3path.DestState, error) {
	// 空 key 表示 bucket 本身，视为目录
	if strings.TrimSuffix(key, "/") == "" {
		return s3path.DestDir, nil
	}

	probe := strings.TrimSuffix(key, "/")

	// 1) HeadObject 命中即为文件
	_, err := c.S3.HeadObject(c.Ctx, bucket, probe, "")
	if err == nil {
		return s3path.DestFile, nil
	}

	// 仅对 404 继续目录探测；403 等直接返回错误
	if apiErr, ok := errors.AsType[*api.ErrorResponse](err); ok {
		switch apiErr.Code {
		case "NoSuchKey", "NotFound", "404":
			// 继续探测目录
		case "AccessDenied", "Forbidden", "403":
			return s3path.DestNone, fmt.Errorf(i18n.T("access denied to bucket '%s'", "拒绝访问存储桶 '%s'"), bucket)
		default:
			return s3path.DestNone, fmt.Errorf("s3 error: %w", err)
		}
	} else {
		return s3path.DestNone, fmt.Errorf("s3 error: %w", err)
	}

	// 2) 目录探测：prefix = key + "/"
	listResp, err := c.S3.ListObjectsV2(c.Ctx, bucket, &api.ListObjectsV2Options{
		Prefix:    probe + "/",
		Delimiter: "/",
		MaxKeys:   1,
	})
	if err != nil {
		return s3path.DestNone, err
	}
	if len(listResp.CommonPrefixes) > 0 || len(listResp.Contents) > 0 {
		return s3path.DestDir, nil
	}

	return s3path.DestNone, nil
}

// Cred 描述一份 S3 凭证 (用于跨端 mirror/diff 判断是否同 endpoint).
type Cred struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	BaseEndpoint    string
}

// GetS3Credentials 返回底层 S3 后端持有的凭证与 endpoint (经 S3Operations 访问器).
func (c *Action) GetS3Credentials() (Cred, error) {
	if c.S3 == nil {
		return Cred{}, fmt.Errorf("s3 client is nil")
	}
	return Cred{
		AccessKeyID:     c.S3.AccessKey(),
		SecretAccessKey: c.S3.SecretKey(),
		SessionToken:    c.S3.SessionToken(),
		BaseEndpoint:    c.S3.Endpoint(),
	}, nil
}

// errStopIteration 是 forEachObject 的哨兵错误：fn 返回它可提前正常结束遍历
// (不视为错误)，用于实现 limit 提前退出等场景。
var errStopIteration = errors.New("stop iteration")

// forEachObject 遍历 bucket 下指定 prefix 的所有对象 (自动翻页), 对每个对象调用 fn。
// 封装了各处重复的 ListObjectsV2 Paginator 循环样板。fn 返回错误会中断遍历;
// fn 返回 errStopIteration 时提前正常结束 (返回 nil)。
func (c *Action) forEachObject(ctx context.Context, bucket, prefix string, fn func(obj api.ObjectInfo) error) error {
	paginator := c.S3.NewListObjectsV2Paginator(bucket, &api.ListObjectsV2Options{
		Prefix: prefix,
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("list objects: %w", err)
		}
		for _, obj := range page.Contents {
			if err := fn(obj); err != nil {
				if errors.Is(err, errStopIteration) {
					return nil
				}
				return err
			}
		}
	}
	return nil
}

// forEachObjectPage 与 forEachObject 相同, 但按页回调 —— 需要整页信息的调用方
// (如按分隔符汇总 CommonPrefixes) 用它, 避免为了拿 prefix 再手写一遍分页循环。
func (c *Action) forEachObjectPage(ctx context.Context, bucket string, opts *api.ListObjectsV2Options, fn func(page *api.ListObjectsV2Output) error) error {
	paginator := c.S3.NewListObjectsV2Paginator(bucket, opts)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("list objects: %w", err)
		}
		if err := fn(page); err != nil {
			if errors.Is(err, errStopIteration) {
				return nil
			}
			return err
		}
	}
	return nil
}

// VersionEntry 是 forEachVersion 回调的统一视图: 把 ListObjectVersions 响应里的
// Version 与 DeleteMarker 两种节点归一, 调用方用 IsDeleteMarker 区分。
// (两种节点的字段并不一致 —— 删除标记没有 Size/ETag/StorageClass。)
type VersionEntry struct {
	Key            string
	VersionID      string
	LastModified   time.Time
	ETag           string
	Size           int64
	StorageClass   string
	IsLatest       bool
	IsDeleteMarker bool
}

// forEachVersion 遍历 bucket 下指定 prefix 的所有对象版本与删除标记 (自动翻页)。
//
// 版本分页曾在 7 处被手写 (rm --versions / bucket remove --force / find --versions /
// ls --versions / stat / ...), 每处都要重复 KeyMarker/VersionIDMarker 的推进与
// 错误包装。集中到这里后口径只有一份。
func (c *Action) forEachVersion(ctx context.Context, bucket, prefix string, fn func(v VersionEntry) error) error {
	return c.forEachVersionPage(ctx, bucket, prefix, func(page *api.ListObjectVersionsOutput) error {
		for _, v := range page.Versions {
			if err := fn(VersionEntry{
				Key: v.Key, VersionID: v.VersionID, LastModified: v.LastModified,
				ETag: v.ETag, Size: v.Size, StorageClass: v.StorageClass, IsLatest: v.IsLatest,
			}); err != nil {
				return err
			}
		}
		for _, m := range page.DeleteMarkers {
			if err := fn(VersionEntry{
				Key: m.Key, VersionID: m.VersionID, LastModified: m.LastModified,
				IsLatest: m.IsLatest, IsDeleteMarker: true,
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

// forEachVersionPage 与 forEachVersion 相同, 但按页回调 (需要区分 Versions 与
// DeleteMarkers 两组的调用方用它)。
func (c *Action) forEachVersionPage(ctx context.Context, bucket, prefix string, fn func(page *api.ListObjectVersionsOutput) error) error {
	paginator := c.S3.NewListObjectVersionsPaginator(bucket, &api.ListObjectVersionsOptions{
		Prefix: prefix,
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("list versions: %w", err)
		}
		if err := fn(page); err != nil {
			if errors.Is(err, errStopIteration) {
				return nil
			}
			return err
		}
	}
	return nil
}
