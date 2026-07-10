package action

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/s3api"
)

// GetOpt get 命令参数
type GetOptions struct {
	Recursive   bool
	Concurrency int
	PartSizeMB  int
	Range       string // HTTP Range header (e.g. "bytes=0-1023"); 仅对单文件有效
	NoProgress  bool   // 不显示进度条（--quiet）
}

// Get 下载对象
func (c *S3Client) GetObject(opt GetOptions, bucket, prefix, localpath string) error {
	ok, err := c.IsS3File(bucket, prefix)
	if err != nil {
		return fmt.Errorf("check s3 path: %s", FormatAPIError(err))
	}
	if !ok || prefix == "" {
		if !opt.Recursive {
			return fmt.Errorf("source is a directory; use -r/--recursive")
		}
		if opt.Range != "" {
			return fmt.Errorf("--range cannot be used with --recursive")
		}
		return c.downloadDirectory(opt, bucket, prefix, localpath)
	}
	return c.downloadSingleFile(opt, bucket, prefix, localpath)
}

func (c *S3Client) downloadDirectory(opt GetOptions, bucket, key, localpath string) error {
	localBasePath, err := determineLocalBasePath(localpath, bucket, key)
	if err != nil {
		return err
	}

	return RunStream(c.Ctx, StreamConfig{
		Concurrency: opt.Concurrency,
		Label:       "get",
		NoProgress:  opt.NoProgress,
		Count: func(ctx context.Context, add func(n, size int64)) error {
			return c.countS3Prefix(ctx, bucket, key, true, add)
		},
		Scan: func(ctx context.Context, jobs chan<- StreamJob) error {
			paginator := s3api.NewListObjectsV2Paginator(c.S3, bucket, &s3api.ListObjectsV2Options{
				Prefix: key,
			})
			for paginator.HasMorePages() {
				page, err := paginator.NextPage(ctx)
				if err != nil {
					return fmt.Errorf("list %s: %s", c.S3Path(bucket, key), FormatAPIError(err))
				}
				for _, obj := range page.Contents {
					objKey := obj.Key
					if strings.HasSuffix(objKey, "/") && obj.Size == 0 {
						continue
					}
					localFilePath, pathErr := buildLocalFilePath(objKey, key, localBasePath)
					if pathErr != nil {
						continue
					}
					jobs <- StreamJob{
						Src:  objKey,
						Dst:  localFilePath,
						Size: obj.Size,
					}
				}
			}
			return nil
		},
		Work: func(ctx context.Context, job StreamJob, report func(n int64)) error {
			_, err := c.downloadFile(opt, job.Src, job.Dst, bucket, report)
			return err
		},
	})
}

func (c *S3Client) downloadSingleFile(opt GetOptions, bucket, key, localpath string) error {
	localFilePath, err := determineLocalFilePath(localpath, key)
	if err != nil {
		return err
	}

	// --range 直接走 GetObject
	if opt.Range != "" {
		return c.rangeGetObject(bucket, key, localFilePath, opt.Range)
	}

	myprint.Printf("get: %s --> %s ", c.S3Path(bucket, key), localFilePath)
	size, err := c.downloadFile(opt, key, localFilePath, bucket, nil)
	if err != nil {
		myprint.Println("FAILED")
		return fmt.Errorf("download: %s", FormatAPIError(err))
	}
	myprint.Printf("(%s)\n", FormatBytes(size))
	return nil
}

func (c *S3Client) rangeGetObject(bucket, key, localFilePath, rng string) error {
	if err := os.MkdirAll(filepath.Dir(localFilePath), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	out, err := c.S3.GetObject(c.Ctx, bucket, key, &s3api.GetObjectOptions{
		Range: rng,
	})
	if err != nil {
		return fmt.Errorf("range get: %s", FormatAPIError(err))
	}
	defer out.Body.Close()

	file, err := os.Create(localFilePath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer file.Close()

	written, err := file.ReadFrom(out.Body)
	if err != nil {
		os.Remove(localFilePath)
		return fmt.Errorf("write file: %w", err)
	}
	myprint.Printf("get: %s [%s] --> %s (%s)\n",
		c.S3Path(bucket, key), rng, localFilePath, FormatBytes(written))
	return nil
}

func (c *S3Client) downloadFile(opt GetOptions, filekey, localfilePath, bucket string, report func(n int64)) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(localfilePath), 0o755); err != nil {
		return 0, fmt.Errorf("mkdir: %w", err)
	}
	file, err := os.Create(localfilePath)
	if err != nil {
		return 0, fmt.Errorf("create file: %w", err)
	}
	defer file.Close()

	out, err := c.S3.GetObject(c.Ctx, bucket, filekey, nil)
	if err != nil {
		os.Remove(localfilePath)
		return 0, err
	}
	defer out.Body.Close()

	// 有进度回调时，用计数 reader 包装 body 流。
	var body io.Reader = out.Body
	if report != nil {
		body = &countingReader{r: out.Body, report: report}
	}

	n, err := io.Copy(file, body)
	if err != nil {
		os.Remove(localfilePath)
		return 0, fmt.Errorf("write file: %w", err)
	}
	return n, nil
}

// countingReader 包装 io.Reader, 按读取进度实时上报字节增量.
type countingReader struct {
	r      io.Reader
	report func(n int64)
}

func (cr *countingReader) Read(p []byte) (int, error) {
	n, err := cr.r.Read(p)
	if n > 0 && cr.report != nil {
		cr.report(int64(n))
	}
	return n, err
}

// ---- 路径辅助 ----

func determineLocalBasePath(localpath, bucket, key string) (string, error) {
	if localpath != "" {
		info, err := os.Stat(localpath)
		if err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("stat path: %w", err)
		}
		if err == nil && !info.IsDir() {
			return "", fmt.Errorf("%s is not a directory", localpath)
		}
		return localpath, nil
	}
	if key != "" {
		return filepath.Base(key), nil
	}
	return bucket, nil
}

func determineLocalFilePath(localpath, key string) (string, error) {
	if localpath == "" {
		return filepath.Base(key), nil
	}
	info, err := os.Stat(localpath)
	if err == nil {
		if info.IsDir() {
			return filepath.Join(localpath, filepath.Base(key)), nil
		}
		return localpath, nil
	}
	if os.IsNotExist(err) {
		parent := filepath.Dir(localpath)
		if parent != "." && parent != "/" {
			if pinfo, perr := os.Stat(parent); perr != nil || !pinfo.IsDir() {
				return "", fmt.Errorf("parent directory %s does not exist", parent)
			}
		}
		return localpath, nil
	}
	return "", fmt.Errorf("stat path: %w", err)
}

func buildLocalFilePath(s3Key, s3Prefix, localBasePath string) (string, error) {
	s3Key = strings.TrimPrefix(s3Key, "/")
	s3Prefix = strings.TrimPrefix(s3Prefix, "/")
	if s3Prefix == "" {
		return filepath.Join(localBasePath, s3Key), nil
	}
	// 去掉前缀及其后可选的斜杠, 等价于原正则 "^s3Prefix/?", 但避免下载热路径重复编译正则.
	relativePath := s3Key
	if strings.HasPrefix(s3Key, s3Prefix) {
		relativePath = strings.TrimPrefix(strings.TrimPrefix(s3Key, s3Prefix), "/")
	}
	if relativePath == "" {
		return localBasePath, nil
	}
	return filepath.Join(localBasePath, relativePath), nil
}
