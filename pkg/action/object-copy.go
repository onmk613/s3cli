package action

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"strings"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/utils"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Cp 复制对象 / 目录
// 处理同对象存储之内的复制
func (c *S3Client) CopyObjects(srcBucket, srcKey, destBucket, destKey string, recursive bool, scrollMax int) error {
	ok, err := c.IsS3File(srcBucket, srcKey)
	if err != nil {
		return fmt.Errorf("check source: %s", FormatAPIError(err))
	}
	ok2, err2 := c.IsS3File(destBucket, destKey)
	if err2 != nil {
		myprint.Warnf("check destination: %s\n", FormatAPIError(err2))
	}

	if !ok && !recursive {
		return fmt.Errorf("source is a directory; use -r/--recursive")
	}
	if ok {
		dst := utils.ResolveDestKey(destKey, destKey, path.Base(srcKey))
		if err := c.copyObject(srcBucket, srcKey, destBucket, dst); err != nil {
			return err
		}
		myprint.Successf("cp: %s -> %s\n", c.S3Path(srcBucket, srcKey), c.S3Path(destBucket, dst))
		return nil
	}
	target := destKey
	if strings.HasSuffix(destKey, "/") && !ok2 {
		target = path.Join(destKey, path.Base(strings.TrimSuffix(srcKey, "/")))
	}
	return c.copyDirStreaming(srcBucket, srcKey, destBucket, target, scrollMax)
}

func (c *S3Client) copyObject(srcBucket, srcKey, destBucket, destKey string) error {
	myprint.Info("copying %s -> %s", c.S3Path(srcBucket, srcKey), c.S3Path(destBucket, destKey))
	_, err := c.S3.CopyObject(c.Ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(destBucket),
		CopySource: aws.String(srcBucket + "/" + srcKey),
		Key:        aws.String(destKey),
	})
	if err != nil {
		return fmt.Errorf("copy: %s", FormatAPIError(err))
	}
	return nil
}

// copyDirStreaming 流式列出并并发复制，带进度条。
func (c *S3Client) copyDirStreaming(srcBucket, srcKey, destBucket, destKey string, scrollMax int) error {
	return RunStream(c.Ctx, StreamConfig{
		Concurrency: 10,
		ScrollMax:   scrollMax,
		Label:       "cp",
		Scan: func(ctx context.Context, jobs chan<- StreamJob) error {
			paginator := s3.NewListObjectsV2Paginator(c.S3, &s3.ListObjectsV2Input{
				Bucket: aws.String(srcBucket), Prefix: aws.String(srcKey),
			})
			for paginator.HasMorePages() {
				page, err := paginator.NextPage(ctx)
				if err != nil {
					return fmt.Errorf("list %s: %s", c.S3Path(srcBucket, srcKey), FormatAPIError(err))
				}
				for _, item := range page.Contents {
					src := aws.ToString(item.Key)
					dst, err := buildDestKey(src, srcKey, destKey)
					if err != nil {
						continue
					}
					jobs <- StreamJob{
						Src:  src,
						Dst:  c.S3Path(destBucket, dst),
						Size: aws.ToInt64(item.Size),
					}
				}
			}
			return nil
		},
		Work: func(ctx context.Context, job StreamJob) error {
			dstKey, _ := buildDestKey(job.Src, srcKey, destKey)
			return c.copyObject(srcBucket, job.Src, destBucket, dstKey)
		},
	})
}

func buildDestKey(srcKey, srcPrefix, destPrefix string) (string, error) {
	srcKey = strings.TrimPrefix(srcKey, "/")
	srcPrefix = strings.TrimPrefix(srcPrefix, "/")
	destPrefix = strings.TrimPrefix(destPrefix, "/")
	if srcPrefix == "" {
		return path.Join(destPrefix, srcKey), nil
	}
	re, err := regexp.Compile("^" + regexp.QuoteMeta(srcPrefix) + "/?")
	if err != nil {
		return "", fmt.Errorf("invalid prefix pattern: %w", err)
	}
	rel := re.ReplaceAllString(srcKey, "")
	if rel == "" {
		return destPrefix, nil
	}
	return path.Join(destPrefix, rel), nil
}
