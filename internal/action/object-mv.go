// object-mv.go 实现同 endpoint 内的对象移动 Mv (mv) = copy + delete 源;
// 参数 (--recursive/-r / --storage-class/--sc / --tags / --metadata).

package action

import (
	"context"
	"errors"
	"fmt"
	"path"
	"s3cli/internal/s3path"
	"strings"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/i18n"
	"s3cli/pkg/s3iface"
)

// Mv 移动对象 = copy + delete
// 处理同对象存储之内的移动
func (c *Action) Mv(opt CopyOptions, srcBucket, srcKey, destBucket, destKey string) error {
	srcTrailing := strings.HasSuffix(srcKey, "/")
	destTrailing := strings.HasSuffix(destKey, "/")

	srcIsFile, err := c.IsS3File(srcBucket, srcKey)
	if err != nil {
		return fmt.Errorf("check source: %s", FormatAPIError(err))
	}
	if !srcIsFile && !opt.Recursive {
		return errors.New(i18n.T("source is a directory; use -r/--recursive", "源是目录；请使用 -r/--recursive"))
	}

	// 单文件源：规则 5/6
	if srcIsFile {
		dst := s3path.ResolveFileDest(destKey, destTrailing, path.Base(strings.TrimSuffix(srcKey, "/")))
		if err := c.mvObject(opt, srcBucket, srcKey, destBucket, dst); err != nil {
			return err
		}
		myprint.PrintfGreen(i18n.T("mv: %s -> %s\n", "移动：%s -> %s\n"), c.S3Path(srcBucket, srcKey), c.S3Path(destBucket, dst))
		return nil
	}

	// 目录源：规则 1/2/3/4
	state, err := c.DestStateOf(destBucket, destKey)
	if err != nil {
		myprint.PrintfYellow("check destination (treated as not-exist): %s\n", FormatAPIError(err))
		state = s3path.DestNone
	}
	destPrefix, appendRel := s3path.ResolveDirDestPrefix(srcKey, srcTrailing, destKey, destTrailing, state)
	return c.mvDirStreaming(opt, srcBucket, srcKey, destBucket, destPrefix, appendRel)
}

func (c *Action) mvObject(opt CopyOptions, srcBucket, srcKey, destBucket, destKey string) error {
	if err := c.copyObject(opt, srcBucket, srcKey, destBucket, destKey); err != nil {
		return err
	}

	_, err := c.S3.DeleteObject(c.Ctx, srcBucket, srcKey, "")
	if err != nil {
		return fmt.Errorf("delete source: %s", FormatAPIError(err))
	}
	return nil
}

// mvDirStreaming 流式列出并并发移动，带进度条。
func (c *Action) mvDirStreaming(opt CopyOptions, srcBucket, srcKey, destBucket, destPrefix string, appendRel bool) error {
	return RunStream(c.Ctx, StreamConfig{
		Concurrency: defaultConcurrency,
		Label:       "mv",
		NoProgress:  opt.NoProgress,
		Count: func(ctx context.Context, add func(n, size int64)) error {
			// 预统计同样跳过目录占位对象, 与 Scan/Count 的计数口径一致。
			return c.countS3Prefix(ctx, srcBucket, srcKey, true, add)
		},
		Scan: func(ctx context.Context, jobs chan<- StreamJob) error {
			return c.forEachObject(ctx, srcBucket, srcKey, func(obj s3iface.ObjectInfo) error {
				// 跳过 0 字节的目录占位对象 ("dir/" 形态), 与 get 的扫描一致:
				// 这类对象没有内容可移动, 复制过去只会留下无意义的目录标记。
				if strings.HasSuffix(obj.Key, "/") && obj.Size == 0 {
					return nil
				}
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
			if err := c.copyObject(opt, srcBucket, job.Src, destBucket, dstKey); err != nil {
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
