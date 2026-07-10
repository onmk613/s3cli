package action

import (
	"fmt"
	"io"
	"os"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/s3api"
)

// PipeOptions pipe 命令参数
type PipeOptions struct {
	ContentType     string
	DefaultMimeType string // 从配置读取的默认 MIME 类型
	Concurrency     int
	PartSizeMB      int
	StorageClass    string
	Metadata        map[string]string
}

// PipeUpload 从 stdin 读取数据并上传到 s3://bucket/key
func (c *S3Client) PipeUpload(opt PipeOptions, bucket, key string) error {
	if key == "" {
		return fmt.Errorf("pipe requires an object key")
	}

	if opt.ContentType == "" {
		if opt.DefaultMimeType != "" {
			opt.ContentType = opt.DefaultMimeType
		} else {
			opt.ContentType = "binary/octet-stream"
		}
	}

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}

	putOpts := &s3api.PutObjectOptions{
		ContentType:  opt.ContentType,
		StorageClass: opt.StorageClass,
		Metadata:     opt.Metadata,
	}

	if _, err := c.S3.PutObject(c.Ctx, bucket, key, data, putOpts); err != nil {
		return fmt.Errorf("pipe upload %s: %s", c.S3Path(bucket, key), FormatAPIError(err))
	}

	myprint.PrintfBoldGreen("pipe: stdin --> %s  (%s)\n", c.S3Path(bucket, key), opt.ContentType)
	return nil
}
