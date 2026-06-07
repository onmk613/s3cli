package action

import (
	"bufio"
	"fmt"
	"os"

	myprint "s3cli/pkg/fmtutil"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
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

	uploader := manager.NewUploader(c.S3, func(u *manager.Uploader) {
		if opt.PartSizeMB > 0 {
			u.PartSize = int64(opt.PartSizeMB) * 1024 * 1024
		}
		if opt.Concurrency > 0 {
			u.Concurrency = opt.Concurrency
		}
	})

	in := &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        bufio.NewReader(os.Stdin),
		ContentType: aws.String(opt.ContentType),
	}
	if opt.StorageClass != "" {
		in.StorageClass = types.StorageClass(opt.StorageClass)
	}
	if len(opt.Metadata) > 0 {
		in.Metadata = opt.Metadata
	}

	if _, err := uploader.Upload(c.Ctx, in); err != nil {
		return fmt.Errorf("pipe upload %s: %s", c.S3Path(bucket, key), FormatAPIError(err))
	}
	myprint.Printf("pipe: stdin --> %s  (%s)\n", c.S3Path(bucket, key), opt.ContentType)
	return nil
}
