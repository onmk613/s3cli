package action

import (
	"context"
	"fmt"
	"mime"
	"os"
	"path"
	"path/filepath"
	"strings"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/s3api"
)

// PutOptions put 命令参数
type PutOptions struct {
	Recursive       bool
	ContentType     string
	DefaultMimeType string // 从配置读取的默认 MIME 类型
	Concurrency     int
	PartSizeMB      int
	StorageClass    string            // e.g. "STANDARD_IA", "GLACIER"
	Metadata        map[string]string // 用户元数据 (x-amz-meta-*)
	NoProgress      bool              // 不显示进度条（--quiet）
}

// PutObject 上传本地文件或目录到 S3
func (c *S3Client) PutObject(opt PutOptions, bucket, prefix, localPath string, isS3Dir bool) error {
	AddMime()
	if opt.Concurrency <= 0 {
		opt.Concurrency = 10
	}

	// 判定本地路径是否为目录：目录必须走流式批量上传，且需要 -r。
	fi, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("stat %s: %w", localPath, err)
	}
	if fi.IsDir() {
		if !opt.Recursive {
			return fmt.Errorf("%s is a directory (use -r to upload recursively)", localPath)
		}
		return c.uploadDirStreaming(opt, bucket, prefix, localPath)
	}

	mimeType := detectMime(localPath, opt.DefaultMimeType)
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
		dstKey = filepath.Base(localPath)
	} else if isS3Dir {
		dstKey = path.Join(prefix, filepath.Base(localPath))
	}
	if err := c.uploadFile(c.Ctx, opt, mimeType, bucket, dstKey, localPath, nil); err != nil {
		return err
	}
	myprint.Printf("put: %s --> %s  (%s)\n", localPath, c.S3Path(bucket, dstKey), mimeType)
	return nil
}

// uploadDirStreaming 流式扫描本地目录并上传，带进度条。
func (c *S3Client) uploadDirStreaming(opt PutOptions, bucket, key, localPath string) error {
	return RunStream(c.Ctx, StreamConfig{
		Concurrency: opt.Concurrency,
		Label:       "put",
		NoProgress:  opt.NoProgress,
		Count: func(ctx context.Context, add func(n, size int64)) error {
			return countLocalDir(localPath, add)
		},
		Scan: func(ctx context.Context, jobs chan<- StreamJob) error {
			return filepath.Walk(localPath, func(p string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if info.IsDir() {
					return nil
				}
				relPath, relErr := filepath.Rel(localPath, p)
				if relErr != nil {
					return relErr
				}
				// S3 key 一律用正斜杠，且只存"纯 key"（不含 alias/bucket 前缀），
				// 显示用的完整路径在日志处再由 S3Path 拼接，避免把展示路径误当 key 上传。
				var dstKey string
				if strings.HasSuffix(localPath, "/") {
					dstKey = path.Join(key, filepath.ToSlash(relPath))
				} else {
					dstKey = path.Join(key, filepath.Base(localPath), filepath.ToSlash(relPath))
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
			return c.uploadFile(ctx, opt, mimeType, bucket, job.Dst, job.Src, report)
		},
	})
}

func (c *S3Client) uploadFile(ctx context.Context, opt PutOptions, mimeType, bucket, fileKey, filePath string, report func(n int64)) error {
	// 流式上传: 打开文件句柄直接传给 s3api, 避免整个文件读入内存 (大文件不再 OOM).
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open %s: %w", filePath, err)
	}
	defer func(f *os.File) {
		_ = f.Close()
	}(f)

	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat %s: %w", filePath, err)
	}

	putOpts := &s3api.PutObjectOptions{
		ContentType:  mimeType,
		StorageClass: opt.StorageClass,
		Metadata:     opt.Metadata,
	}

	var uploadErr error
	if fi.Size() >= multipartThreshold {
		uploadErr = c.uploadMultipartFile(ctx, bucket, fileKey, filePath, f, fi, opt.PartSizeMB, putOpts, report)
	} else {
		_, uploadErr = c.S3.PutObjectStream(ctx, bucket, fileKey, f, fi.Size(), putOpts)
	}
	if uploadErr != nil {
		return fmt.Errorf("upload %s: %s", filePath, FormatAPIError(uploadErr))
	}
	if report != nil && fi.Size() < multipartThreshold {
		report(fi.Size())
	}
	return nil
}

func detectMime(localPath string, defaultMime string) string {
	ext := strings.ToLower(filepath.Ext(localPath))
	if m := mime.TypeByExtension(ext); m != "" {
		return m
	}
	if defaultMime != "" {
		return defaultMime
	}
	return "binary/octet-stream"
}
