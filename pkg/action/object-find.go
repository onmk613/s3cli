package action

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/s3api"
)

// FindOptions find 命令参数
type FindOptions struct {
	Name      string // glob/regex; 当 NameRegex=false 时按 shell glob, 否则按 RE2 正则
	NameRegex bool
	MinSize   int64  // 最小字节数 (含)
	MaxSize   int64  // 最大字节数 (含, 0 = 不限制)
	NewerThan string // RFC3339 时间; 仅返回 LastModified > t 的对象
	OlderThan string // RFC3339 时间; 仅返回 LastModified < t 的对象
	Limit     int    // 最多输出多少条, 0 = 不限制
}

// FindObjects 按条件搜索 s3://bucket/prefix 下的对象
func (c *S3Client) FindObjects(opt FindOptions, bucket, prefix string) error {
	if bucket == "" {
		return fmt.Errorf("find requires a bucket")
	}

	var nameRe *regexp.Regexp
	if opt.Name != "" {
		pattern := opt.Name
		if !opt.NameRegex {
			pattern = globToRegex(pattern)
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return fmt.Errorf("invalid --name pattern %q: %w", opt.Name, err)
		}
		nameRe = re
	}

	var newer, older time.Time
	var err error
	if opt.NewerThan != "" {
		newer, err = parseTime(opt.NewerThan)
		if err != nil {
			return fmt.Errorf("--newer-than: %w", err)
		}
	}
	if opt.OlderThan != "" {
		older, err = parseTime(opt.OlderThan)
		if err != nil {
			return fmt.Errorf("--older-than: %w", err)
		}
	}

	var matched int
	var totalSize int64
	var limitReached bool
	err = c.forEachObject(c.Ctx, bucket, prefix, func(obj s3api.ObjectInfo) error {
		key := obj.Key
		size := obj.Size

		if opt.MinSize > 0 && size < opt.MinSize {
			return nil
		}
		if opt.MaxSize > 0 && size > opt.MaxSize {
			return nil
		}
		if !newer.IsZero() && !obj.LastModified.After(newer) {
			return nil
		}
		if !older.IsZero() && !obj.LastModified.Before(older) {
			return nil
		}
		if nameRe != nil {
			base := key
			if i := strings.LastIndex(key, "/"); i >= 0 {
				base = key[i+1:]
			}
			if !nameRe.MatchString(base) {
				return nil
			}
		}

		myprint.PrintfDim("[%s]  ", obj.LastModified.Format("2006-01-02 15:04:05"))
		myprint.Printf("%12d   ", size)
		myprint.PrintfGreen("FILE  %s\n", c.S3Path(bucket, key))

		matched++
		totalSize += size
		if opt.Limit > 0 && matched >= opt.Limit {
			limitReached = true
			return errStopIteration
		}
		return nil
	})
	if err != nil {
		return err
	}
	if limitReached {
		myprint.PrintfYellow("\n(limit %d reached)\n", opt.Limit)
		return nil
	}
	myprint.PrintfBoldBlue("\n%d matching objects (%s)\n", matched, FormatBytes(totalSize))
	return nil
}

// globToRegex 把简单的 shell glob (* ? [abc]) 转换为 RE2 正则
func globToRegex(g string) string {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(g); i++ {
		c := g[i]
		switch c {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		case '.', '+', '(', ')', '{', '}', '|', '^', '$', '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteString("$")
	return b.String()
}

// parseTime 支持 RFC3339 / "2006-01-02" / "2006-01-02 15:04:05"
func parseTime(s string) (time.Time, error) {
	layouts := []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized time format %q (use RFC3339 or 'YYYY-MM-DD')", s)
}
