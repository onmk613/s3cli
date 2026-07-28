// object-pipe.go 实现从 stdin 流式上传 (PipeUpload), 适合管道场景;
// 输入大小未知, 小输入走单次 PUT, 大输入自动转分片上传.

package action

import (
	"fmt"
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

	putOpts := &s3api.PutObjectOptions{
		ContentType:  opt.ContentType,
		StorageClass: opt.StorageClass,
		Metadata:     opt.Metadata,
	}

	if err := c.uploadUnknownSize(c.Ctx, bucket, key, os.Stdin, opt.PartSizeMB, putOpts); err != nil {
		return fmt.Errorf("pipe upload %s: %s", c.S3Path(bucket, key), FormatAPIError(err))
	}

	myprint.PrintfBoldGreen("pipe: stdin --> %s  (%s)\n", c.S3Path(bucket, key), opt.ContentType)
	return nil
}
