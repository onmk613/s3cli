package action

import (
	"fmt"
	"strings"
	"time"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/s3api"
)

// SignurlOpt signurl 参数
type SignurlOptions struct {
	ExpireSeconds int
	Method        string // GET / PUT / DELETE / HEAD
	SignurlV2     bool
}

// Signurl 生成预签名 URL
func (c *S3Client) Signurl(opt SignurlOptions, bucketname, key string) error {
	method := strings.ToUpper(strings.TrimSpace(opt.Method))
	if method == "" {
		method = "GET"
	}

	if method == "GET" || method == "HEAD" {
		ok, err := c.IsS3File(bucketname, key)
		if err != nil {
			return fmt.Errorf("check s3 path: %s", FormatAPIError(err))
		}
		if !ok {
			return fmt.Errorf("%s: not a file", c.S3Path(bucketname, key))
		}
	}

	// s3api.Client 内部已持有凭证, 无需在此显式取凭证。

	var signed string
	var err error
	if opt.SignurlV2 {
		signed, err = c.S3.PresignV2(c.Ctx, bucketname, key, method, int64(opt.ExpireSeconds))
	} else {
		if opt.ExpireSeconds > 604800 {
			myprint.PrintfYellow("Warning: v4 signature maximum validity is 7 days (604800s), the generated URL may expire earlier\n")
		}
		signed, err = c.S3.PresignedURL(c.Ctx, bucketname, key, &s3api.PresignOptions{
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
