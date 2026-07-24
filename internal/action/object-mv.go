package action

import (
	"context"
	"fmt"
	"path"
	"s3cli/internal/s3path"
	"strings"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/s3api"
)

// Mv 移动对象 = copy + delete
// 处理同对象存储之内的移动
func (c *S3Client) Mv(srcBucket, srcKey, destBucket, destKey string, recursive, noProgress bool) error {
	srcTrailing := strings.HasSuffix(srcKey, "/")
	destTrailing := strings.HasSuffix(destKey, "/")

	srcIsFile, err := c.IsS3File(srcBucket, srcKey)
	if err != nil {
		return fmt.Errorf("check source: %s", FormatAPIError(err))
	}
	if !srcIsFile && !recursive {
		return fmt.Errorf("source is a directory; use -r/--recursive")
	}

	// 单文件源：规则 5/6
	if srcIsFile {
		dst := s3path.ResolveFileDest(destKey, destTrailing, path.Base(strings.TrimSuffix(srcKey, "/")))
		if err := c.mvObject(srcBucket, srcKey, destBucket, dst); err != nil {
			return err
		}
		myprint.PrintfGreen("mv: %s -> %s\n", c.S3Path(srcBucket, srcKey), c.S3Path(destBucket, dst))
		return nil
	}

	// 目录源：规则 1/2/3/4
	state, err := c.DestStateOf(destBucket, destKey)
	if err != nil {
		myprint.PrintfYellow("check destination (treated as not-exist): %s\n", FormatAPIError(err))
		state = s3path.DestNone
	}
	destPrefix, appendRel := s3path.ResolveDirDestPrefix(srcKey, srcTrailing, destKey, destTrailing, state)
	return c.mvDirStreaming(srcBucket, srcKey, destBucket, destPrefix, appendRel, noProgress)
}

func (c *S3Client) mvObject(srcBucket, srcKey, destBucket, destKey string) error {
	if err := c.copyObject(srcBucket, srcKey, destBucket, destKey); err != nil {
		return err
	}

	_, err := c.S3.DeleteObject(c.Ctx, srcBucket, srcKey, "")
	if err != nil {
		return fmt.Errorf("delete source: %s", FormatAPIError(err))
	}
	return nil
}

// mvDirStreaming 流式列出并并发移动，带进度条。
func (c *S3Client) mvDirStreaming(srcBucket, srcKey, destBucket, destPrefix string, appendRel, noProgress bool) error {
	return RunStream(c.Ctx, StreamConfig{
		Concurrency: defaultConcurrency,
		Label:       "mv",
		NoProgress:  noProgress,
		Count: func(ctx context.Context, add func(n, size int64)) error {
			return c.countS3Prefix(ctx, srcBucket, srcKey, false, add)
		},
		Scan: func(ctx context.Context, jobs chan<- StreamJob) error {
			return c.forEachObject(ctx, srcBucket, srcKey, func(obj s3api.ObjectInfo) error {
				dst := buildDestKey(obj.Key, srcKey, destPrefix, appendRel)
				jobs <- StreamJob{
					Src:  obj.Key,
					Dst:  c.S3Path(destBucket, dst),
					Size: obj.Size,
				}
				return nil
			})
		},
		Work: func(ctx context.Context, job StreamJob, _ func(n int64)) error {
			dstKey := buildDestKey(job.Src, srcKey, destPrefix, appendRel)
			if err := c.copyObject(srcBucket, job.Src, destBucket, dstKey); err != nil {
				return err
			}
			_, err := c.S3.DeleteObject(ctx, srcBucket, job.Src, "")
			if err != nil {
				return fmt.Errorf("delete source: %s", FormatAPIError(err))
			}
			return nil
		},
	})
}
