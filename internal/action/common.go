package action

import (
	"context"
	"errors"
	"fmt"
	"s3cli/internal/s3path"
	"strings"

	"s3cli/pkg/s3api"
)

// defaultConcurrency 是流式传输（get/put/cp/mv）与 mirror/diff 的默认并发数。
// 与 config.DefaultConcurrency 保持一致；action 不依赖 config 以维持与配置层解耦。
const defaultConcurrency = 10

// S3Client 封装自建的 s3api.Client, 持有 alias 和 ctx.
type S3Client struct {
	S3    *s3api.Client
	Alias string
	Ctx   context.Context
}

// S3Path 格式化路径为 "alias:bucket/key", 和命令行格式一样
func (c *S3Client) S3Path(bucket, key string) string {
	if key == "" {
		return c.Alias + ":" + bucket
	}
	return c.Alias + ":" + bucket + "/" + key
}

// S3PathStatic 静态版本，无需 S3Client 实例
func S3PathStatic(alias, bucket, key string) string {
	if key == "" {
		return alias + ":" + bucket
	}
	return alias + ":" + bucket + "/" + key
}

// IsS3File 检查路径是文件 (true) 还是目录 / 不存在 (false)
func (c *S3Client) IsS3File(bucket, key string) (bool, error) {
	// 空 key 表示目标是 bucket 本身，不可能是文件
	if key == "" {
		return false, nil
	}

	_, err := c.S3.HeadObject(c.Ctx, bucket, key, "")
	if err == nil {
		return true, nil
	}

	// 按 ErrorResponse 的 Code 判断
	var apiErr *s3api.ErrorResponse
	if errors.As(err, &apiErr) {
		switch apiErr.Code {
		case "NoSuchKey", "NotFound", "404":
			// 对象不存在：可能是目录前缀，继续探测。
			return c.checkIfDirectory(bucket, key)
		case "AccessDenied", "Forbidden", "403":
			return false, fmt.Errorf("access denied to bucket '%s'", bucket)
		}
	}
	return false, fmt.Errorf("s3 error: %w", err)
}

// objectExists 仅判断对象是否存在 (true=存在), 不做目录前缀探测。
// 用于上传前的存在性检查: 目标要么是文件对象, 要么不存在。
// 与 IsS3File 不同, 404 直接判 false, 不再探测是否为目录前缀;
// 403 等权限错误以 error 返回 (无法确认存在性时宁可报错, 不静默上传)。
func (c *S3Client) objectExists(ctx context.Context, bucket, key string) (bool, error) {
	_, err := c.S3.HeadObject(ctx, bucket, key, "")
	if err == nil {
		return true, nil
	}
	var apiErr *s3api.ErrorResponse
	if errors.As(err, &apiErr) {
		switch apiErr.Code {
		case "NoSuchKey", "NotFound", "404":
			return false, nil
		}
	}
	return false, err
}

// checkIfDirectory 在 HeadObject 返回 404 后判断 key 是否为目录前缀。
// 返回 (false, nil) 表示是目录前缀（非文件），(false, err) 表示路径不存在。
func (c *S3Client) checkIfDirectory(bucket, key string) (bool, error) {
	// 1) 先按标准目录探测：prefix = key + "/"。
	listResp, err := c.S3.ListObjectsV2(c.Ctx, bucket, &s3api.ListObjectsV2Options{
		Prefix:    key + "/",
		Delimiter: "/",
		MaxKeys:   1,
	})
	if err != nil {
		return false, err
	}
	if len(listResp.CommonPrefixes) > 0 || len(listResp.Contents) > 0 {
		return false, nil
	}

	// 2) 兜底：用裸 key 作为前缀探测（覆盖无尾斜杠的伪目录前缀，
	//    例如 key="dir/name" 实际匹配 "dir/name/..." 或 "dir/name-xxx"）。
	listResp2, err := c.S3.ListObjectsV2(c.Ctx, bucket, &s3api.ListObjectsV2Options{
		Prefix:  key,
		MaxKeys: 1,
	})
	if err != nil {
		return false, err
	}
	if len(listResp2.Contents) > 0 {
		return false, nil
	}
	return false, fmt.Errorf("path '%s' does not exist in bucket '%s'", key, bucket)
}

// DestStateOf 判断目标 key 当前的状态：文件 / 目录 / 不存在。
// 用于 cp/mv/mirror 计算目标对象 key。探测失败时返回 (DestNone, err)。
func (c *S3Client) DestStateOf(bucket, key string) (s3path.DestState, error) {
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
	var apiErr *s3api.ErrorResponse
	if errors.As(err, &apiErr) {
		switch apiErr.Code {
		case "NoSuchKey", "NotFound", "404":
			// 继续探测目录
		case "AccessDenied", "Forbidden", "403":
			return s3path.DestNone, fmt.Errorf("access denied to bucket '%s'", bucket)
		default:
			return s3path.DestNone, fmt.Errorf("s3 error: %w", err)
		}
	} else {
		return s3path.DestNone, fmt.Errorf("s3 error: %w", err)
	}

	// 2) 目录探测：prefix = key + "/"
	listResp, err := c.S3.ListObjectsV2(c.Ctx, bucket, &s3api.ListObjectsV2Options{
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

type Cred struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	BaseEndpoint    string
}

func (c *S3Client) GetS3Credentials() (Cred, error) {
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
func (c *S3Client) forEachObject(ctx context.Context, bucket, prefix string, fn func(obj s3api.ObjectInfo) error) error {
	paginator := s3api.NewListObjectsV2Paginator(c.S3, bucket, &s3api.ListObjectsV2Options{
		Prefix: prefix,
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("list objects: %s", FormatAPIError(err))
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
