package action

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	myprint "s3cli/pkg/fmtutil"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// GetOpt get 命令参数
type GetOptions struct {
	Recursive   bool
	Concurrency int
	PartSizeMB  int
	Range       string // HTTP Range header (e.g. "bytes=0-1023"); 仅对单文件有效
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
		Count: func(ctx context.Context, add func(n, size int64)) error {
			return c.countS3Prefix(ctx, bucket, key, true, add)
		},
		Scan: func(ctx context.Context, jobs chan<- StreamJob) error {
			paginator := s3.NewListObjectsV2Paginator(c.S3, &s3.ListObjectsV2Input{
				Bucket: aws.String(bucket), Prefix: aws.String(key),
			})
			for paginator.HasMorePages() {
				page, err := paginator.NextPage(ctx)
				if err != nil {
					return fmt.Errorf("list %s: %s", c.S3Path(bucket, key), FormatAPIError(err))
				}
				for _, obj := range page.Contents {
					objKey := aws.ToString(obj.Key)
					if strings.HasSuffix(objKey, "/") && aws.ToInt64(obj.Size) == 0 {
						continue
					}
					localFilePath, pathErr := buildLocalFilePath(objKey, key, localBasePath)
					if pathErr != nil {
						continue
					}
					jobs <- StreamJob{
						Src:  objKey,
						Dst:  localFilePath,
						Size: aws.ToInt64(obj.Size),
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

	// --range 直接走 GetObject (manager 不支持 Range)
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
	out, err := c.S3.GetObject(c.Ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket), Key: aws.String(key), Range: aws.String(rng),
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

	downloader := manager.NewDownloader(c.S3, func(d *manager.Downloader) {
		if opt.PartSizeMB > 0 {
			d.PartSize = int64(opt.PartSizeMB) * 1024 * 1024
		}
		if opt.Concurrency > 0 {
			d.Concurrency = opt.Concurrency
		}
	})

	// 有进度回调时，用计数包装器包装目标文件，使 manager 并发写入分片时实时上报字节。
	var dst io.WriterAt = file
	if report != nil {
		dst = NewDownloadCounter(file, report)
	}

	n, err := downloader.Download(c.Ctx, dst, &s3.GetObjectInput{
		Bucket: aws.String(bucket), Key: aws.String(filekey),
	})
	if err != nil {
		os.Remove(localfilePath)
		return 0, err
	}
	return n, nil
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
	re, err := regexp.Compile("^" + regexp.QuoteMeta(s3Prefix) + "/?")
	if err != nil {
		return "", fmt.Errorf("invalid prefix pattern: %w", err)
	}
	relativePath := re.ReplaceAllString(s3Key, "")
	if relativePath == "" {
		return localBasePath, nil
	}
	return filepath.Join(localBasePath, relativePath), nil
}
