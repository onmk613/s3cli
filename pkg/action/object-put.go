package action

import (
	"context"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"

	myprint "s3cli/pkg/fmtutil"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// PutOpt put 命令参数
type PutOptions struct {
	Recursive       bool
	ContentType     string
	DefaultMimeType string // 从配置读取的默认 MIME 类型
	Concurrency     int
	PartSizeMB      int
	StorageClass    string            // e.g. "STANDARD_IA", "GLACIER"
	Metadata        map[string]string // 用户元数据 (x-amz-meta-*)
}

// Put 上传文件
func (c *S3Client) PutObject(opt PutOptions, bucket, prefix, localpath string, isS3Dir bool) error {
	uploader := manager.NewUploader(c.S3, func(u *manager.Uploader) {
		if opt.PartSizeMB > 0 {
			u.PartSize = int64(opt.PartSizeMB) * 1024 * 1024
		}
		if opt.Concurrency > 0 {
			u.Concurrency = opt.Concurrency
		}
	})

	AddMime()
	if opt.Concurrency <= 0 {
		opt.Concurrency = 10
	}

	if isS3Dir {
		return c.uploadDirStreaming(uploader, opt, bucket, prefix, localpath)
	}

	mimeType := detectMime(localpath, opt.DefaultMimeType)
	if opt.ContentType != "" {
		if _, _, err := mime.ParseMediaType(opt.ContentType); err != nil {
			return fmt.Errorf("invalid --content-type %q: %w", opt.ContentType, err)
		}
		mimeType = opt.ContentType
	}

	dstKey := prefix
	if strings.HasSuffix(prefix, "/") && prefix != "" {
		dstKey = filepath.Join(prefix, filepath.Base(localpath))
	} else if prefix == "" {
		dstKey = filepath.Base(localpath)
	}
	if err := uploadFile(c.Ctx, uploader, opt, mimeType, bucket, dstKey, localpath); err != nil {
		return err
	}
	myprint.Printf("put: %s --> %s  (%s)\n", localpath, c.S3Path(bucket, dstKey), mimeType)
	return nil
}

// uploadDirStreaming 流式扫描本地目录并上传，带进度条。
func (c *S3Client) uploadDirStreaming(u *manager.Uploader, opt PutOptions, bucket, key, localpath string) error {
	return RunStream(c.Ctx, StreamConfig{
		Concurrency: opt.Concurrency,
		Label:       "put",
		Scan: func(ctx context.Context, jobs chan<- StreamJob) error {
			return filepath.Walk(localpath, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if info.IsDir() {
					return nil
				}
				relPath, relErr := filepath.Rel(localpath, path)
				if relErr != nil {
					return relErr
				}
				var dstKey string
				if strings.HasSuffix(localpath, "/") {
					dstKey = filepath.Join(key, relPath)
				} else {
					dstKey = filepath.Join(key, filepath.Base(localpath), relPath)
				}
				jobs <- StreamJob{Src: path, Dst: c.S3Path(bucket, dstKey), Size: info.Size()}
				return nil
			})
		},
		Work: func(ctx context.Context, job StreamJob) error {
			mimeType := detectMime(job.Src, opt.DefaultMimeType)
			if opt.ContentType != "" {
				mimeType = opt.ContentType
			}
			return uploadFile(ctx, u, opt, mimeType, bucket, job.Dst, job.Src)
		},
	})
}

func uploadFile(ctx context.Context, u *manager.Uploader, opt PutOptions, mimeType, bucket, fileKey, filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open %s: %w", filePath, err)
	}
	defer file.Close()

	in := &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(fileKey),
		Body:        file,
		ContentType: aws.String(mimeType),
	}
	if opt.StorageClass != "" {
		in.StorageClass = types.StorageClass(opt.StorageClass)
	}
	if len(opt.Metadata) > 0 {
		in.Metadata = opt.Metadata
	}

	if _, err := u.Upload(ctx, in); err != nil {
		return fmt.Errorf("upload %s: %s", filePath, FormatAPIError(err))
	}
	return nil
}

func detectMime(path string, defaultMime string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if m := mime.TypeByExtension(ext); m != "" {
		return m
	}
	if defaultMime != "" {
		return defaultMime
	}
	return "binary/octet-stream"
}
