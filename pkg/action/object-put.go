package action

import (
	"context"
	"fmt"
	"io"
	"mime"
	"os"
	"path"
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

	// 判定本地路径是否为目录：目录必须走流式批量上传，且需要 -r。
	fi, err := os.Stat(localpath)
	if err != nil {
		return fmt.Errorf("stat %s: %w", localpath, err)
	}
	if fi.IsDir() {
		if !opt.Recursive {
			return fmt.Errorf("%s is a directory (use -r to upload recursively)", localpath)
		}
		return c.uploadDirStreaming(uploader, opt, bucket, prefix, localpath)
	}

	mimeType := detectMime(localpath, opt.DefaultMimeType)
	if opt.ContentType != "" {
		if _, _, err := mime.ParseMediaType(opt.ContentType); err != nil {
			return fmt.Errorf("invalid --content-type %q: %w", opt.ContentType, err)
		}
		mimeType = opt.ContentType
	}

	// 计算目标 key：
	//   - prefix 为空              -> 用本地文件名
	//   - S3 路径以 "/" 结尾(目录)  -> prefix/本地文件名
	//   - 否则                     -> prefix 即完整对象名
	dstKey := prefix
	if prefix == "" {
		dstKey = filepath.Base(localpath)
	} else if isS3Dir {
		dstKey = path.Join(prefix, filepath.Base(localpath))
	}
	if err := uploadFile(c.Ctx, uploader, opt, mimeType, bucket, dstKey, localpath, nil); err != nil {
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
		Count: func(ctx context.Context, add func(n, size int64)) error {
			return countLocalDir(localpath, add)
		},
		Scan: func(ctx context.Context, jobs chan<- StreamJob) error {
			return filepath.Walk(localpath, func(p string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if info.IsDir() {
					return nil
				}
				relPath, relErr := filepath.Rel(localpath, p)
				if relErr != nil {
					return relErr
				}
				// S3 key 一律用正斜杠，且只存"纯 key"（不含 alias/bucket 前缀），
				// 显示用的完整路径在日志处再由 S3Path 拼接，避免把展示路径误当 key 上传。
				var dstKey string
				if strings.HasSuffix(localpath, "/") {
					dstKey = path.Join(key, filepath.ToSlash(relPath))
				} else {
					dstKey = path.Join(key, filepath.Base(localpath), filepath.ToSlash(relPath))
				}
				jobs <- StreamJob{Src: p, Dst: dstKey, Size: info.Size()}
				return nil
			})
		},
		Work: func(ctx context.Context, job StreamJob, report func(n int64)) error {
			mimeType := detectMime(job.Src, opt.DefaultMimeType)
			if opt.ContentType != "" {
				mimeType = opt.ContentType
			}
			return uploadFile(ctx, u, opt, mimeType, bucket, job.Dst, job.Src, report)
		},
	})
}

func uploadFile(ctx context.Context, u *manager.Uploader, opt PutOptions, mimeType, bucket, fileKey, filePath string, report func(n int64)) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open %s: %w", filePath, err)
	}
	defer file.Close()

	// 有进度回调时，用计数包装器包装文件，使 manager 并发读取分片时实时上报字节。
	var body io.Reader = file
	if report != nil {
		body = NewUploadCounter(file, report)
	}

	in := &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(fileKey),
		Body:        body,
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

func detectMime(localpath string, defaultMime string) string {
	ext := strings.ToLower(filepath.Ext(localpath))
	if m := mime.TypeByExtension(ext); m != "" {
		return m
	}
	if defaultMime != "" {
		return defaultMime
	}
	return "binary/octet-stream"
}
