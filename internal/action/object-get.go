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

// GetOptions get 命令参数
type GetOptions struct {
	Recursive   bool
	Concurrency int
	PartSizeMB  int
	Range       string // HTTP Range header (e.g. "bytes=0-1023"); 仅对单文件有效
	NoProgress  bool   // 不显示进度条（--quiet）
}

// GetObject 下载对象
func (c *S3Client) GetObject(opt GetOptions, bucket, prefix, localPath string) error {
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
		return c.downloadDirectory(opt, bucket, prefix, localPath)
	}
	return c.downloadSingleFile(opt, bucket, prefix, localPath)
}

func (c *S3Client) downloadDirectory(opt GetOptions, bucket, key, localPath string) error {
	localBasePath, err := determineLocalBasePath(localPath, bucket, key)
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
			return c.forEachObject(ctx, bucket, key, func(obj s3api.ObjectInfo) error {
				objKey := obj.Key
				if strings.HasSuffix(objKey, "/") && obj.Size == 0 {
					return nil
				}
				localFilePath, pathErr := buildLocalFilePath(objKey, key, localBasePath)
				if pathErr != nil {
					return nil
				}
				jobs <- StreamJob{
					Src:  objKey,
					Dst:  localFilePath,
					Size: obj.Size,
				}
				return nil
			})
		},
		Work: func(ctx context.Context, job StreamJob, report func(n int64)) error {
			_, err := c.downloadFile(job.Src, job.Dst, bucket, report)
			return err
		},
	})
}

func (c *S3Client) downloadSingleFile(opt GetOptions, bucket, key, localPath string) error {
	localFilePath, err := determineLocalFilePath(localPath, key)
	if err != nil {
		return err
	}

	// --range 直接走 GetObject
	if opt.Range != "" {
		return c.rangeGetObject(bucket, key, localFilePath, opt.Range)
	}

	myprint.Printf("get: %s --> %s ", c.S3Path(bucket, key), localFilePath)
	size, err := c.downloadFile(key, localFilePath, bucket, nil)
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
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(out.Body)

	file, err := os.CreateTemp(filepath.Dir(localFilePath), ".s3cli-download-*")
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	tmpPath := file.Name()
	defer os.Remove(tmpPath)
	defer func(file *os.File) {
		_ = file.Close()
	}(file)

	written, err := file.ReadFrom(out.Body)
	if err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close file: %w", err)
	}
	if err := os.Rename(tmpPath, localFilePath); err != nil {
		return fmt.Errorf("replace file: %w", err)
	}
	myprint.Printf("get: %s [%s] --> %s (%s)\n",
		c.S3Path(bucket, key), rng, localFilePath, FormatBytes(written))
	return nil
}

func (c *S3Client) downloadFile(key, localPath, bucket string, report func(n int64)) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return 0, fmt.Errorf("mkdir: %w", err)
	}
	file, err := os.CreateTemp(filepath.Dir(localPath), ".s3cli-download-*")
	if err != nil {
		return 0, fmt.Errorf("create file: %w", err)
	}
	tmpPath := file.Name()
	defer os.Remove(tmpPath)
	defer func(file *os.File) {
		_ = file.Close()
	}(file)

	out, err := c.S3.GetObject(c.Ctx, bucket, key, nil)
	if err != nil {
		return 0, err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(out.Body)

	// 有进度回调时，用计数 reader 包装 body 流。
	var body io.Reader = out.Body
	if report != nil {
		body = &countingReader{r: out.Body, report: report}
	}

	n, err := io.Copy(file, body)
	if err != nil {
		return 0, fmt.Errorf("write file: %w", err)
	}
	if err := file.Close(); err != nil {
		return 0, fmt.Errorf("close file: %w", err)
	}
	if err := os.Rename(tmpPath, localPath); err != nil {
		return 0, fmt.Errorf("replace file: %w", err)
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

func determineLocalBasePath(localPath, bucket, key string) (string, error) {
	if localPath != "" {
		info, err := os.Stat(localPath)
		if err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("stat path: %w", err)
		}
		if err == nil && !info.IsDir() {
			return "", fmt.Errorf("%s is not a directory", localPath)
		}
		return localPath, nil
	}
	if key != "" {
		return filepath.Base(key), nil
	}
	return bucket, nil
}

func determineLocalFilePath(localPath, key string) (string, error) {
	if localPath == "" {
		return filepath.Base(key), nil
	}
	info, err := os.Stat(localPath)
	if err == nil {
		if info.IsDir() {
			return filepath.Join(localPath, filepath.Base(key)), nil
		}
		return localPath, nil
	}
	if os.IsNotExist(err) {
		parent := filepath.Dir(localPath)
		if parent != "." && parent != "/" {
			if fileInfo, err := os.Stat(parent); err != nil || !fileInfo.IsDir() {
				return "", fmt.Errorf("parent directory %s does not exist", parent)
			}
		}
		return localPath, nil
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
