// object-find.go 实现对象条件搜索 FindObjects, 参数与 mc find 对齐:
// --name (glob) / --regex (RE2) / --path (目录名 glob) / --larger / --smaller /
// --newer-than / --older-than (时长或绝对时间) / --maxdepth / --ignore / --print / --limit.

package action

import (
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/s3iface"
)

// FindOptions find 命令参数 (mc find 对齐).
type FindOptions struct {
	Name      string   // 对象名 glob 匹配 (作用于 basename)
	Regex     string   // RE2 正则, 作用于完整 key
	Path      string   // 目录名 glob 匹配 (作用于路径中的目录段)
	Larger    int64    // 大于此大小的对象 (0 = 不限制)
	Smaller   int64    // 小于此大小的对象 (0 = 不限制)
	NewerThan string   // 时长 (如 7d10h31s) 或绝对时间; 仅返回修改时间更新的对象
	OlderThan string   // 时长或绝对时间; 仅返回修改时间更早的对象
	MaxDepth  int      // 限制目录遍历深度 (0 = 不限制)
	Ignore    []string // 排除匹配的通配符模式
	Print     string   // 自定义输出格式, 支持 {name}/{size}/{time}/{url}/{path}
	Limit     int      // 最多输出多少条, 0 = 不限制
	JSON      bool     // --json: JSON lines 输出
}

// FindObjects 按条件搜索 s3://bucket/prefix 下的对象
func (c *Action) FindObjects(opt FindOptions, bucket, prefix string) error {
	if bucket == "" {
		return fmt.Errorf("find requires a bucket")
	}

	var nameRe, regexRe, pathRe *regexp.Regexp
	var err error
	if opt.Name != "" {
		nameRe, err = regexp.Compile(globToRegex(opt.Name))
		if err != nil {
			return fmt.Errorf("invalid --name pattern %q: %w", opt.Name, err)
		}
	}
	if opt.Regex != "" {
		regexRe, err = regexp.Compile(opt.Regex)
		if err != nil {
			return fmt.Errorf("invalid --regex pattern %q: %w", opt.Regex, err)
		}
	}
	if opt.Path != "" {
		pathRe, err = regexp.Compile(globToRegex(opt.Path))
		if err != nil {
			return fmt.Errorf("invalid --path pattern %q: %w", opt.Path, err)
		}
	}

	var ignoreRes []*regexp.Regexp
	for _, ig := range opt.Ignore {
		re, err := regexp.Compile(globToRegex(ig))
		if err != nil {
			return fmt.Errorf("invalid --ignore pattern %q: %w", ig, err)
		}
		ignoreRes = append(ignoreRes, re)
	}

	// 时间过滤: 兼容 mc 时长 ("7d10h31s") 与绝对时间 (RFC3339 / YYYY-MM-DD)
	newer, err := parseFilterTime(opt.NewerThan)
	if err != nil {
		return fmt.Errorf("--newer-than: %w", err)
	}
	older, err := parseFilterTime(opt.OlderThan)
	if err != nil {
		return fmt.Errorf("--older-than: %w", err)
	}

	var matched int
	var totalSize int64
	var limitReached bool
	err = c.forEachObject(c.Ctx, bucket, prefix, func(obj s3iface.ObjectInfo) error {
		key := obj.Key
		size := obj.Size

		if opt.Larger > 0 && size <= opt.Larger {
			return nil
		}
		if opt.Smaller > 0 && size >= opt.Smaller {
			return nil
		}
		if !newer.IsZero() && !obj.LastModified.After(newer) {
			return nil
		}
		if !older.IsZero() && !obj.LastModified.Before(older) {
			return nil
		}
		if opt.MaxDepth > 0 && keyDepth(key, prefix) > opt.MaxDepth {
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
		if regexRe != nil && !regexRe.MatchString(key) {
			return nil
		}
		if pathRe != nil && !matchDirPath(pathRe, key, prefix) {
			return nil
		}
		for _, re := range ignoreRes {
			if re.MatchString(key) {
				return nil
			}
		}

		matched++
		totalSize += size
		if opt.JSON {
			if err := printJSONLine(map[string]any{
				"path":         c.S3Path(bucket, key),
				"size":         size,
				"lastModified": obj.LastModified,
			}); err != nil {
				return err
			}
		} else if opt.Print != "" {
			myprint.Println(formatFindPrint(opt.Print, c.S3Path(bucket, key), key, size, obj.LastModified))
		} else {
			myprint.PrintfDim("[%s]  ", obj.LastModified.Format("2006-01-02 15:04:05"))
			myprint.Printf("%12d   ", size)
			myprint.PrintfGreen("FILE  %s\n", c.S3Path(bucket, key))
		}
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
		if opt.JSON {
			return nil
		}
		myprint.PrintfYellow("\n(limit %d reached)\n", opt.Limit)
		return nil
	}
	if opt.JSON {
		return nil
	}
	myprint.PrintfBoldBlue("\n%d matching objects (%s)\n", matched, FormatBytes(totalSize))
	return nil
}

// keyDepth 计算 key 相对 prefix 的层级深度.
func keyDepth(key, prefix string) int {
	rel := strings.TrimPrefix(key, prefix)
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" {
		return 0
	}
	return strings.Count(rel, "/") + 1
}

// matchDirPath 判断 key 的路径 (不含 basename) 是否命中目录名 glob.
func matchDirPath(re *regexp.Regexp, key, prefix string) bool {
	rel := strings.TrimPrefix(key, prefix)
	rel = strings.TrimPrefix(rel, "/")
	dir := path.Dir(rel)
	if dir == "." || dir == "/" {
		return false
	}
	for _, seg := range strings.Split(dir, "/") {
		if re.MatchString(seg) {
			return true
		}
	}
	return false
}

// formatFindPrint 按 {name}/{size}/{time}/{url}/{path} 占位符格式化输出 (mc find --print).
func formatFindPrint(format, url, key string, size int64, t time.Time) string {
	name := key
	if i := strings.LastIndex(key, "/"); i >= 0 {
		name = key[i+1:]
	}
	s := format
	s = strings.ReplaceAll(s, "{name}", name)
	s = strings.ReplaceAll(s, "{path}", key)
	s = strings.ReplaceAll(s, "{url}", url)
	s = strings.ReplaceAll(s, "{size}", fmt.Sprintf("%d", size))
	s = strings.ReplaceAll(s, "{time}", t.Format("2006-01-02 15:04:05"))
	return s
}

// parseFilterTime 解析 mc 风格时长 ("7d10h31s") 或绝对时间 (RFC3339 / 'YYYY-MM-DD' / 'YYYY-MM-DD HH:MM:SS').
// 返回过滤基准时间; 时长为相对当前时间的过去时间点.
func parseFilterTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	if d, err := ParseDuration(s); err == nil {
		return time.Now().Add(-d), nil
	}
	return parseTime(s)
}

// ParseDuration 解析 mc 时长串, 如 "7d10h31s" / "30m" / "2d" (d=天 h=时 m=分 s=秒).
func ParseDuration(s string) (time.Duration, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}
	var total time.Duration
	num := ""
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= '0' && c <= '9' {
			num += string(c)
			continue
		}
		if num == "" {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		var unit time.Duration
		switch c {
		case 'd':
			unit = 24 * time.Hour
		case 'h':
			unit = time.Hour
		case 'm':
			unit = time.Minute
		case 's':
			unit = time.Second
		default:
			return 0, fmt.Errorf("invalid duration unit %q in %q (use d/h/m/s)", string(c), s)
		}
		var n int64
		if _, err := fmt.Sscanf(num, "%d", &n); err != nil {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		total += time.Duration(n) * unit
		num = ""
	}
	if num != "" {
		return 0, fmt.Errorf("invalid duration %q (trailing number)", s)
	}
	if total <= 0 {
		return 0, fmt.Errorf("duration must be positive: %q", s)
	}
	return total, nil
}

// globToRegex 把简单的 shell glob (* ? [abc] [!a]) 转换为 RE2 正则
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
		case '[':
			// 字符类: glob 的 [!...] 取反对应正则的 [^...],
			// 原样透传会被 RE2 理解为字面 '!' (语义相反)。
			b.WriteByte('[')
			if i+1 < len(g) && g[i+1] == '!' {
				b.WriteByte('^')
				i++
			}
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
	return time.Time{}, fmt.Errorf("unrecognized time format %q (use duration like 7d10h31s or RFC3339/'YYYY-MM-DD')", s)
}
