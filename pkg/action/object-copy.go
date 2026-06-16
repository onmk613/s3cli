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

// 处理同对象存储之内的复制
func (c *S3Client) CopyObjects(srcBucket, srcKey, destBucket, destKey string, recursive bool) error {
	srcTrailing := strings.HasSuffix(srcKey, "/")
	destTrailing := strings.HasSuffix(destKey, "/")

	// 源 key 是文件还是目录
	srcIsFile, err := c.IsS3File(srcBucket, srcKey)
	if err != nil {
		return fmt.Errorf("check source: %s", FormatAPIError(err))
	}
	// 为目录但是没有设置 -r
	if !srcIsFile && !recursive {
		return fmt.Errorf("source is a directory; use -r/--recursive")
	}

	// 单文件源
	if srcIsFile {
		dst := utils.ResolveFileDest(destKey, destTrailing, path.Base(strings.TrimSuffix(srcKey, "/")))
		if err := c.copyObject(srcBucket, srcKey, destBucket, dst); err != nil {
			return err
		}
		myprint.PrintfGreen("cp: %s -> %s\n", c.S3Path(srcBucket, srcKey), c.S3Path(destBucket, dst))
		return nil
	}

	// 目录源
	state, err := c.DestStateOf(destBucket, destKey)
	if err != nil {
		myprint.PrintfYellow("check destination (treated as not-exist): %s\n", FormatAPIError(err))
		state = utils.DestNone
	}
	destPrefix, appendRel := utils.ResolveDirDestPrefix(srcKey, srcTrailing, destKey, destTrailing, state)
	return c.copyDirStreaming(srcBucket, srcKey, destBucket, destPrefix, appendRel)
}

func (c *S3Client) copyObject(srcBucket, srcKey, destBucket, destKey string) error {
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
// destPrefix 为目标前缀；appendRel=true 时把每个源对象相对源前缀的路径拼到 destPrefix 之下，
// 否则所有源对象都写到 destPrefix（与规则 1/3 的 trailing-none/file 语义一致）。
func (c *S3Client) copyDirStreaming(srcBucket, srcKey, destBucket, destPrefix string, appendRel bool) error {
	return RunStream(c.Ctx, StreamConfig{
		Concurrency: 10,
		Label:       "cp",
		Count: func(ctx context.Context, add func(n, size int64)) error {
			return c.countS3Prefix(ctx, srcBucket, srcKey, false, add)
		},
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
					dst := buildDestKey(src, srcKey, destPrefix, appendRel)
					jobs <- StreamJob{
						Src:  src,
						Dst:  c.S3Path(destBucket, dst),
						Size: aws.ToInt64(item.Size),
					}
				}
			}
			return nil
		},
		Work: func(ctx context.Context, job StreamJob, _ func(n int64)) error {
			dstKey := buildDestKey(job.Src, srcKey, destPrefix, appendRel)
			return c.copyObject(srcBucket, job.Src, destBucket, dstKey)
		},
	})
}

// buildDestKey 计算目录复制时单个源对象的目标 key。
//
//	srcKey      源对象绝对 key
//	srcPrefix   源前缀（可带尾斜杠）
//	destPrefix  目标前缀（不含尾斜杠）
//	appendRel   是否把源对象相对源前缀的路径拼到目标前缀下
//
// appendRel=false 时所有对象都写到 destPrefix 自身。
func buildDestKey(srcKey, srcPrefix, destPrefix string, appendRel bool) string {
	srcKey = strings.TrimPrefix(srcKey, "/")
	srcPrefix = strings.Trim(srcPrefix, "/")
	destPrefix = strings.Trim(destPrefix, "/")

	if !appendRel {
		return destPrefix
	}

	rel := srcKey
	if srcPrefix != "" {
		re := regexp.MustCompile("^" + regexp.QuoteMeta(srcPrefix) + "/?")
		rel = re.ReplaceAllString(srcKey, "")
	}
	if rel == "" {
		return destPrefix
	}
	return path.Join(destPrefix, rel)
}
