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

	"s3cli/internal/api"
	myprint "s3cli/internal/fmtutil"
	"s3cli/internal/i18n"
)

// Mv 移动对象 = copy + delete
// 处理同对象存储之内的移动
func (c *Action) Mv(opt CopyOptions, srcBucket, srcKey, destBucket, destKey string) error {
	if opt.Concurrency <= 0 {
		opt.Concurrency = defaultConcurrency
	}
	if err := validateConcurrency(opt.Concurrency); err != nil {
		return err
	}
	srcTrailing := strings.HasSuffix(srcKey, "/")
	destTrailing := strings.HasSuffix(destKey, "/")

	srcIsFile, err := c.IsS3File(srcBucket, srcKey)
	if err != nil {
		return fmt.Errorf("check source: %w", err)
	}
	if !srcIsFile && !opt.Recursive {
		return errors.New(i18n.T("source is a directory; use -r/--recursive", "源是目录；请使用 -r/--recursive"))
	}

	// 单文件源：规则 5/6
	if srcIsFile {
		dst := s3path.ResolveFileDest(destKey, destTrailing, path.Base(strings.TrimSuffix(srcKey, "/")))
		// dst 可能与源解析成同一个对象: `mv a:b/dir/f.txt a:b/dir/` 会由
		// ResolveFileDest 还原出 "dir/f.txt", `mv a:b/k a:b` 会还原出 "k"。
		// mv = copy + delete, 自拷贝成功后紧跟的删除会把对象本身抹掉 —— 对象
		// 直接消失。这里 fail-fast, 而不是静默当成空操作: 用户按 `mv 文件 目录/`
		// 的直觉操作时, 真实意图几乎不会是"删除这个对象"。
		if err := checkSameObject(srcBucket, srcKey, destBucket, dst); err != nil {
			return err
		}
		if opt.DryRun {
			size, sizeErr := c.headObjectSize(srcBucket, srcKey, "")
			if sizeErr != nil {
				return sizeErr
			}
			c.dryRunReport(i18n.T("mv", "移动"), c.S3Path(srcBucket, srcKey), c.S3Path(destBucket, dst), size)
			return nil
		}
		if err := c.mvObject(opt, srcBucket, srcKey, destBucket, dst); err != nil {
			return err
		}
		myprint.PrintfGreen(i18n.T("mv: %s -> %s\n", "移动：%s -> %s\n"), c.S3Path(srcBucket, srcKey), c.S3Path(destBucket, dst))
		return nil
	}

	// 目录源：规则 1/2/3/4
	state, err := c.DestStateOf(destBucket, destKey)
	if err != nil {
		myprint.PrintfYellow("check destination (treated as not-exist): %s\n", err)
		state = s3path.DestNone
	}
	if state == s3path.DestFile {
		return fmt.Errorf(i18n.T("%s: destination exists and is a file object; cannot move a directory onto it",
			"%s：目标已存在且是文件对象；目录无法移动到单个对象上"), c.S3Path(destBucket, destKey))
	}
	destPrefix, appendRel := s3path.ResolveDirDestPrefix(srcKey, srcTrailing, destKey, destTrailing, state)
	if !appendRel {
		return fmt.Errorf(i18n.T("%s: target of a directory move must be an existing directory or end with '/'",
			"%s：目录移动的目标必须是已存在的目录或以 '/' 结尾"), c.S3Path(destBucket, destKey))
	}
	if err := checkDirPrefixOverlap(srcBucket, srcKey, destBucket, destPrefix); err != nil {
		return err
	}
	// 目录源的列举前缀必须规范化为 "dir/": 裸前缀会连带列出兄弟目录
	// ("logs-2023/x"), 而 mv 复制成功后还会删除这些源对象 —— 一次移动
	// 就毁掉无关前缀的数据。cp/get 同样依赖本规范化。
	srcKey = normalizeDirPrefix(srcKey)
	return c.mvDirStreaming(opt, srcBucket, srcKey, destBucket, destPrefix, appendRel)
}

// checkSameObject 在「同桶 + 同 key」时返回错误。
//
// mv 的实现是 copy 后 delete 源, 一旦目标解析回源对象, 删除就会把对象本身
// 抹掉 (且自拷贝本身是合法 S3 请求, 不会失败), 因此必须在发请求前拦截。
// 比较时只去尾部的 "/": 前导 "/" 在 S3 key 里是有意义的差异, 不能一并抹平。
func checkSameObject(srcBucket, srcKey, destBucket, destKey string) error {
	if srcBucket != destBucket {
		return nil
	}
	if strings.TrimSuffix(srcKey, "/") != strings.TrimSuffix(destKey, "/") {
		return nil
	}
	return fmt.Errorf(i18n.T(
		"source and destination resolve to the same object (%s/%s); nothing to move — specify a different destination",
		"源与目标解析为同一个对象（%s/%s）；无需移动——请指定不同的目标"),
		srcBucket, strings.TrimSuffix(srcKey, "/"))
}

func (c *Action) mvObject(opt CopyOptions, srcBucket, srcKey, destBucket, destKey string) error {
	// 兜底: 任何调用路径都不允许自移动。
	if err := checkSameObject(srcBucket, srcKey, destBucket, destKey); err != nil {
		return err
	}
	if err := c.copyObject(opt, srcBucket, srcKey, destBucket, destKey); err != nil {
		return err
	}

	_, err := c.S3.DeleteObject(c.Ctx, srcBucket, srcKey, "")
	if err != nil {
		return fmt.Errorf("delete source: %w", err)
	}
	return nil
}

// mvDirStreaming 流式列出并并发移动，带进度条。
func (c *Action) mvDirStreaming(opt CopyOptions, srcBucket, srcKey, destBucket, destPrefix string, appendRel bool) error {
	return RunStream(c.Ctx, StreamConfig{
		Concurrency: opt.Concurrency,
		Label:       "mv",
		NoProgress:  opt.NoProgress,
		Count: func(ctx context.Context, add func(n, size int64)) error {
			// 预统计同样跳过目录占位对象, 与 Scan/Count 的计数口径一致。
			return c.countS3Prefix(ctx, srcBucket, srcKey, true, add)
		},
		Scan: func(ctx context.Context, jobs chan<- StreamJob) error {
			return c.forEachObject(ctx, srcBucket, srcKey, func(obj api.ObjectInfo) error {
				// 跳过 0 字节的目录占位对象 ("dir/" 形态), 与 get 的扫描一致:
				// 这类对象没有内容可移动, 复制过去只会留下无意义的目录标记。
				if strings.HasSuffix(obj.Key, "/") && obj.Size == 0 {
					return nil
				}
				if !matchesMirrorFilters(relKeyForDelete(obj.Key, srcKey), opt.Include, opt.Exclude) {
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
			if opt.DryRun {
				c.dryRunReport(i18n.T("mv", "移动"), c.S3Path(srcBucket, job.Src), c.S3Path(destBucket, dstKey), job.Size)
				return nil
			}
			if err := c.copyObject(opt, srcBucket, job.Src, destBucket, dstKey); err != nil {
				return err
			}
			_, err := c.S3.DeleteObject(ctx, srcBucket, job.Src, "")
			if err != nil {
				return fmt.Errorf("delete source: %w", err)
			}
			return nil
		},
	})
}
