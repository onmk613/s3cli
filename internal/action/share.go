package action

import (
	"fmt"
	"strings"
	"time"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/s3api"
)

// ShareOptions Share 参数
type ShareOptions struct {
	ExpireSeconds int
	Method        string // GET / PUT / DELETE / HEAD
	SignV2        bool
}

// Share 生成预签名 URL
func (c *S3Client) Share(opt ShareOptions, bucket, key string) error {
	method := strings.ToUpper(strings.TrimSpace(opt.Method))
	if method == "" {
		method = "GET"
	}

	// 统一校验过期时间: v4 对 <=0 会静默取 15 分钟、v2 直接报错,
	// 在入口处拒绝, 保证同一 flag 两种签名行为一致。
	if opt.ExpireSeconds <= 0 {
		return fmt.Errorf("--expire must be positive seconds, got %d", opt.ExpireSeconds)
	}
	// v4 预签名最长 7 天; 提前在这里报错, 而不是生成到一半才失败。
	if !opt.SignV2 && opt.ExpireSeconds > 604800 {
		return fmt.Errorf("--expire %ds exceeds v4 signature maximum of 7 days (604800s)", opt.ExpireSeconds)
	}

	if method == "GET" || method == "HEAD" {
		ok, err := c.IsS3File(bucket, key)
		if err != nil {
			return fmt.Errorf("check s3 path: %s", FormatAPIError(err))
		}
		if !ok {
			return fmt.Errorf("%s: not a file", c.S3Path(bucket, key))
		}
	}

	// s3api.Client 内部已持有凭证, 无需在此显式取凭证。

	var signed string
	var err error
	if opt.SignV2 {
		signed, err = c.S3.PresignV2(c.Ctx, bucket, key, method, int64(opt.ExpireSeconds))
	} else {
		signed, err = c.S3.PresignedURL(c.Ctx, bucket, key, &s3api.PresignOptions{
			Method:  method,
			Expires: time.Duration(opt.ExpireSeconds) * time.Second,
		})
	}
	if err != nil {
		return fmt.Errorf("presign: %s", FormatAPIError(err))
	}

	myprint.PrintlnGreen(signed)
	return nil
}
