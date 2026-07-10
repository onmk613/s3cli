package action

import (
	"fmt"
	"io"
	"os"

	"s3cli/pkg/s3api"
)

// CatOptions cat 命令参数
type CatOptions struct {
	Range string // HTTP Range header (e.g. "bytes=0-1023")
}

func (c *S3Client) CatObject(opt CatOptions, bucket, key string) error {
	if key == "" {
		return fmt.Errorf("cat requires an object key, not a bucket")
	}

	opts := &s3api.GetObjectOptions{}
	if opt.Range != "" {
		opts.Range = opt.Range
	}

	out, err := c.S3.GetObject(c.Ctx, bucket, key, opts)
	if err != nil {
		return fmt.Errorf("cat %s: %s", c.S3Path(bucket, key), FormatAPIError(err))
	}
	defer out.Body.Close()

	if _, err := io.Copy(os.Stdout, out.Body); err != nil {
		return fmt.Errorf("write stdout: %w", err)
	}
	return nil
}
