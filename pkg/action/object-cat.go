package action

import (
	"fmt"
	"io"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// CatOptions cat 命令参数
type CatOptions struct {
	Range string // HTTP Range header (e.g. "bytes=0-1023")
}

// CatObject 把对象内容直接写到 stdout
func (c *S3Client) CatObject(opt CatOptions, bucket, key string) error {
	if key == "" {
		return fmt.Errorf("cat requires an object key, not a bucket")
	}

	in := &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}
	if opt.Range != "" {
		in.Range = aws.String(opt.Range)
	}

	out, err := c.S3.GetObject(c.Ctx, in)
	if err != nil {
		return fmt.Errorf("cat %s: %s", c.S3Path(bucket, key), FormatAPIError(err))
	}
	defer out.Body.Close()

	if _, err := io.Copy(os.Stdout, out.Body); err != nil {
		return fmt.Errorf("write stdout: %w", err)
	}
	return nil
}
