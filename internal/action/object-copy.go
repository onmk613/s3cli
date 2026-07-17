package action

import (
	"context"
	"fmt"
	"path"
	"s3cli/internal/utils"
	"strings"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/s3api"
)

// CopyObjects 处理同对象存储之内的复制
func (c *S3Client) CopyObjects(srcBucket, srcKey, destBucket, destKey string, recursive, noProgress bool) error {
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
	return c.copyDirStreaming(srcBucket, srcKey, destBucket, destPrefix, appendRel, noProgress)
}

func (c *S3Client) copyObject(srcBucket, srcKey, destBucket, destKey string) error {
	_, err := c.S3.CopyObject(c.Ctx, srcBucket, srcKey, destBucket, destKey, nil)
	if err != nil {
		return fmt.Errorf("copy: %s", FormatAPIError(err))
	}
	return nil
}

// copyDirStreaming 流式列出并并发复制，带进度条。
// destPrefix 为目标前缀；appendRel=true 时把每个源对象相对源前缀的路径拼到 destPrefix 之下，
// 否则所有源对象都写到 destPrefix（与规则 1/3 的 trailing-none/file 语义一致）。
func (c *S3Client) copyDirStreaming(srcBucket, srcKey, destBucket, destPrefix string, appendRel, noProgress bool) error {
	return RunStream(c.Ctx, StreamConfig{
		Concurrency: defaultConcurrency,
		Label:       "cp",
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
	if srcPrefix != "" && strings.HasPrefix(srcKey, srcPrefix) {
		// 去掉前缀及其后可选的斜杠, 等价于原正则 "^srcPrefix/?", 但避免热路径重复编译正则.
		rel = strings.TrimPrefix(strings.TrimPrefix(srcKey, srcPrefix), "/")
	}
	if rel == "" {
		return destPrefix
	}
	return path.Join(destPrefix, rel)
}
