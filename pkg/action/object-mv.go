package action

import (
	"context"
	"fmt"
	"path"
	"strings"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/utils"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Mv 移动对象 = copy + delete
// 处理同对象存储之内的移动
func (c *S3Client) Mv(srcBucket, srcKey, destBucket, destKey string, recursive bool, scrollMax int) error {
	ok, err := c.IsS3File(srcBucket, srcKey)
	if err != nil {
		return fmt.Errorf("check source: %s", FormatAPIError(err))
	}
	ok2, err2 := c.IsS3File(destBucket, destKey)
	if err2 != nil {
		myprint.Warn("check destination: %s", FormatAPIError(err2))
	}

	if !ok && !recursive {
		return fmt.Errorf("source is a directory; use -r/--recursive")
	}
	if ok {
		dst := utils.ResolveDestKey(destKey, destKey, path.Base(srcKey))
		return c.mvObject(srcBucket, srcKey, destBucket, dst)
	}
	target := destKey
	if strings.HasSuffix(destKey, "/") && !ok2 {
		target = path.Join(destKey, path.Base(strings.TrimSuffix(srcKey, "/")))
	}
	return c.mvDirStreaming(srcBucket, srcKey, destBucket, target, scrollMax)
}

func (c *S3Client) mvObject(srcBucket, srcKey, destBucket, destKey string) error {
	if err := c.copyObject(srcBucket, srcKey, destBucket, destKey); err != nil {
		return err
	}
	_, err := c.S3.DeleteObject(c.Ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(srcBucket), Key: aws.String(srcKey),
	})
	if err != nil {
		return fmt.Errorf("delete source: %s", FormatAPIError(err))
	}
	return nil
}

// mvDirStreaming 流式列出并并发移动，带进度条。
func (c *S3Client) mvDirStreaming(srcBucket, srcKey, destBucket, destKey string, scrollMax int) error {
	return RunStream(c.Ctx, StreamConfig{
		Concurrency: 10,
		ScrollMax:   scrollMax,
		Label:       "mv",
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
			if err := c.copyObject(srcBucket, job.Src, destBucket, dstKey); err != nil {
				return err
			}
			_, err := c.S3.DeleteObject(ctx, &s3.DeleteObjectInput{
				Bucket: aws.String(srcBucket), Key: aws.String(job.Src),
			})
			if err != nil {
				return fmt.Errorf("delete source: %s", FormatAPIError(err))
			}
			return nil
		},
	})
}
