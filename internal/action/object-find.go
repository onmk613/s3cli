// object-find.go 实现对象条件搜索 FindObjects, 支持:
// --name (glob) / --regex (RE2) / --path (目录名 glob) / --larger / --smaller /
// --newer-than / --older-than (时长或绝对时间) / --min-depth/--max-depth /
// --type / --storage-class / --include/--exclude / --ignore /
// --sort/--reverse / --print / --limit.

package action

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/i18n"
	"s3cli/pkg/s3iface"
)

// FindOptions find 命令参数.
type FindOptions struct {
	Name         string   // 对象名 glob 匹配 (作用于 basename)
	Regex        string   // RE2 正则, 作用于完整 key
	Path         string   // 目录名 glob 匹配 (作用于路径中的目录段)
	Larger       int64    // 大于此大小的对象 (0 = 不限制)
	Smaller      int64    // 小于此大小的对象 (0 = 不限制)
	NewerThan    string   // 时长 (如 7d10h31s) 或绝对时间; 仅返回修改时间更新的对象
	OlderThan    string   // 时长或绝对时间; 仅返回修改时间更早的对象
	MinDepth     int      // 限制目录遍历深度下限 (0 = 不限制)
	MaxDepth     int      // 限制目录遍历深度上限 (0 = 不限制)
	Type         string   // 仅匹配 file 或 dir (dir 指 0 字节目录标记对象)
	StorageClass string   // 仅匹配指定存储级别 (如 STANDARD / GLACIER)
	Include      []string // 仅匹配任一 glob 的对象
	Exclude      []string // 不匹配任一 glob 的对象
	Ignore       []string // 排除匹配的通配符模式 (与 --exclude 相同, 兼容保留)
	Sort         string   // name / size / time; 前缀 "-" 表示倒序
	Reverse      bool     // 排序取反
	Print        string   // 自定义输出格式, 支持 {name}/{size}/{time}/{url}/{path}/{etag}/{storage-class}/{version-id}
	Limit        int      // 最多输出多少条, 0 = 不限制
	JSON         bool     // --json: JSON lines 输出
	Versions     bool     // --versions: 基于 ListObjectVersions 的最新版本时间过滤 (含 delete marker)
}

// findMatch 记录一个通过过滤的对象 (排序模式需要先收集再输出).
type findMatch struct {
	key          string
	size         int64
	etag         string
	storageClass string
	dirMarker    bool
	modified     time.Time
	versionID    string
	deleteMark   bool
}

// preparedFind 是 FindObjects / findByVersions 共用的解析结果.
type preparedFind struct {
	nameRe, regexRe, pathRe *regexp.Regexp
	ignoreRes               []*regexp.Regexp
	typ                     string
	sortField               string
	sortDesc                bool
	newer, older            time.Time
	filtersActive           bool
}

// prepareFind 校验并解析全部过滤/排序选项.
func prepareFind(opt FindOptions) (*preparedFind, error) {
	p := &preparedFind{}
	var err error
	if opt.Name != "" {
		p.nameRe, err = regexp.Compile(globToRegex(opt.Name))
		if err != nil {
			return nil, fmt.Errorf(i18n.T("invalid --name pattern %q: %w", "无效的 --name 模式 %q：%w"), opt.Name, err)
		}
	}
	if opt.Regex != "" {
		p.regexRe, err = regexp.Compile(opt.Regex)
		if err != nil {
			return nil, fmt.Errorf(i18n.T("invalid --regex pattern %q: %w", "无效的 --regex 模式 %q：%w"), opt.Regex, err)
		}
	}
	if opt.Path != "" {
		p.pathRe, err = regexp.Compile(globToRegex(opt.Path))
		if err != nil {
			return nil, fmt.Errorf(i18n.T("invalid --path pattern %q: %w", "无效的 --path 模式 %q：%w"), opt.Path, err)
		}
	}
	for _, ig := range opt.Ignore {
		re, err := regexp.Compile(globToRegex(ig))
		if err != nil {
			return nil, fmt.Errorf(i18n.T("invalid --ignore pattern %q: %w", "无效的 --ignore 模式 %q：%w"), ig, err)
		}
		p.ignoreRes = append(p.ignoreRes, re)
	}
	if p.typ, err = normalizeFindType(opt.Type); err != nil {
		return nil, err
	}
	if p.sortField, p.sortDesc, err = parseFindSort(opt.Sort); err != nil {
		return nil, err
	}
	if opt.Reverse {
		p.sortDesc = !p.sortDesc
	}
	// 时间过滤: 兼容时长串 ("7d10h31s") 与绝对时间 (RFC3339 / YYYY-MM-DD)
	if p.newer, err = parseFilterTime(opt.NewerThan); err != nil {
		return nil, fmt.Errorf(i18n.T("--newer-than: %w", "--newer-than：%w"), err)
	}
	if p.older, err = parseFilterTime(opt.OlderThan); err != nil {
		return nil, fmt.Errorf(i18n.T("--older-than: %w", "--older-than：%w"), err)
	}
	p.filtersActive = p.typ != "" || opt.StorageClass != "" || len(opt.Include) > 0 ||
		len(opt.Exclude) > 0 || len(opt.Ignore) > 0 || p.nameRe != nil || p.regexRe != nil || p.pathRe != nil ||
		opt.Larger > 0 || opt.Smaller > 0 || !p.newer.IsZero() || !p.older.IsZero() ||
		opt.MinDepth > 0 || opt.MaxDepth > 0
	return p, nil
}

// findPrinter 收集 find 匹配结果并渲染为表格 (text 模式).
type findPrinter struct {
	opt FindOptions
	tbl *myprint.Table
}

// newFindPrinter 创建表格收集器; --json/--print 模式不需要表格.
func newFindPrinter(opt FindOptions) *findPrinter {
	if opt.JSON || opt.Print != "" {
		return nil
	}
	headers := []string{
		i18n.T("Time", "时间"),
		i18n.T("Size", "大小"),
		i18n.T("Type", "类型"),
		i18n.T("Path", "路径"),
	}
	if opt.Versions {
		headers = append(headers, i18n.T("Version ID", "版本ID"))
	}
	return &findPrinter{opt: opt, tbl: myprint.NewTable(headers...).AlignRight(1).PlainRowLimit(lsTableRowLimit)}
}

// add 追加一条匹配到表格.
func (fp *findPrinter) add(c *Action, bucket string, m findMatch) {
	if fp == nil {
		return
	}
	cells := []myprint.Cell{
		{Text: m.modified.Format(lsTimeLayout), Color: myprint.Dim},
		{Text: fmt.Sprintf("%d", m.size)},
	}
	switch {
	case m.deleteMark:
		cells[1] = myprint.Cell{Text: "-"}
		cells = append(cells,
			myprint.Cell{Text: i18n.T("DEL*", "删除*"), Color: myprint.Red},
			myprint.Cell{Text: c.S3Path(bucket, m.key), Color: myprint.Red},
		)
	case m.dirMarker:
		cells[1] = myprint.Cell{Text: "-"}
		cells = append(cells,
			myprint.Cell{Text: i18n.T("DIR", "目录"), Color: myprint.Blue},
			myprint.Cell{Text: c.S3Path(bucket, m.key), Color: myprint.Blue},
		)
	default:
		cells = append(cells,
			myprint.Cell{Text: i18n.T("FILE", "文件"), Color: myprint.Green},
			myprint.Cell{Text: c.S3Path(bucket, m.key), Color: myprint.Green},
		)
	}
	if fp.opt.Versions {
		cells = append(cells, myprint.Cell{Text: m.versionID, Color: myprint.Cyan})
	}
	fp.tbl.AddRow(cells...)
}

// render 输出表格.
func (fp *findPrinter) render() {
	if fp != nil {
		fp.tbl.Render()
	}
}

// FindObjects 按条件搜索 s3://bucket/prefix 下的对象.
// --versions 开启时基于 ListObjectVersions 的"最新版本时间"过滤
// (开 versioning 时含 delete marker 为最新版本的对象; 未开时等价于创建时间).
func (c *Action) FindObjects(opt FindOptions, bucket, prefix string) error {
	if bucket == "" {
		return errors.New(i18n.T("find requires a bucket", "find 需要指定存储桶"))
	}
	p, err := prepareFind(opt)
	if err != nil {
		return err
	}
	if opt.Versions {
		return c.findByVersions(opt, bucket, prefix, p)
	}

	var matched, scanned int
	var totalSize int64
	var limitReached bool
	var collected []findMatch
	fp := newFindPrinter(opt)

	err = c.forEachObject(c.Ctx, bucket, prefix, func(obj s3iface.ObjectInfo) error {
		scanned++
		m, ok := matchFindObject(obj, opt, p, prefix)
		if !ok {
			return nil
		}
		if p.sortField != "" {
			collected = append(collected, m)
			return nil
		}
		if err := emitFindMatch(c, opt, bucket, m, fp); err != nil {
			return err
		}
		matched++
		totalSize += m.size
		if opt.Limit > 0 && matched >= opt.Limit {
			limitReached = true
			return errStopIteration
		}
		return nil
	})
	if err != nil {
		return err
	}

	if p.sortField != "" {
		sortFindMatches(collected, p.sortField, p.sortDesc)
		if opt.Limit > 0 && len(collected) > opt.Limit {
			collected = collected[:opt.Limit]
			limitReached = true
		}
		for _, m := range collected {
			if err := emitFindMatch(c, opt, bucket, m, fp); err != nil {
				return err
			}
			matched++
			totalSize += m.size
		}
	}
	fp.render()

	return printFindSummary(opt, matched, scanned, totalSize, p, limitReached)
}

// findByVersions 基于 ListObjectVersions 遍历, 对每个 key 取最新版本
// (含 delete marker) 的修改时间应用过滤, 输出含 versionId.
func (c *Action) findByVersions(opt FindOptions, bucket, prefix string, p *preparedFind) error {
	latest := map[string]findMatch{}
	var keyOrder []string

	paginator := c.S3.NewListObjectVersionsPaginator(bucket, &s3iface.ListObjectVersionsOptions{Prefix: prefix})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(c.Ctx)
		if err != nil {
			return fmt.Errorf("list versions: %s", FormatAPIError(err))
		}
		for _, v := range page.Versions {
			if !v.IsLatest {
				continue
			}
			if _, seen := latest[v.Key]; !seen {
				keyOrder = append(keyOrder, v.Key)
			}
			latest[v.Key] = findMatch{
				key:          v.Key,
				size:         v.Size,
				etag:         v.ETag,
				storageClass: v.StorageClass,
				dirMarker:    strings.HasSuffix(v.Key, "/") && v.Size == 0,
				modified:     v.LastModified,
				versionID:    v.VersionID,
			}
		}
		for _, m := range page.DeleteMarkers {
			if !m.IsLatest {
				continue
			}
			if _, seen := latest[m.Key]; !seen {
				keyOrder = append(keyOrder, m.Key)
			}
			latest[m.Key] = findMatch{
				key:        m.Key,
				modified:   m.LastModified,
				versionID:  m.VersionID,
				deleteMark: true,
			}
		}
	}

	sort.Strings(keyOrder)
	var matched, scanned int
	var totalSize int64
	var collected []findMatch
	fp := newFindPrinter(opt)
	for _, key := range keyOrder {
		scanned++
		m := latest[key]
		obj := s3iface.ObjectInfo{
			Key:          m.key,
			LastModified: m.modified,
			ETag:         m.etag,
			Size:         m.size,
			StorageClass: m.storageClass,
		}
		fm, ok := matchFindObject(obj, opt, p, prefix)
		if !ok {
			continue
		}
		fm.versionID = m.versionID
		fm.deleteMark = m.deleteMark
		if p.sortField != "" {
			collected = append(collected, fm)
			continue
		}
		if err := emitFindMatch(c, opt, bucket, fm, fp); err != nil {
			return err
		}
		matched++
		totalSize += fm.size
	}

	limitReached := false
	if p.sortField != "" {
		sortFindMatches(collected, p.sortField, p.sortDesc)
		if opt.Limit > 0 && len(collected) > opt.Limit {
			collected = collected[:opt.Limit]
			limitReached = true
		}
		for _, m := range collected {
			if err := emitFindMatch(c, opt, bucket, m, fp); err != nil {
				return err
			}
			matched++
			totalSize += m.size
		}
	}
	fp.render()

	return printFindSummary(opt, matched, scanned, totalSize, p, limitReached)
}

// printFindSummary 输出 find 的汇总行 (limit 提示 / 0 匹配上下文 / 匹配统计).
func printFindSummary(opt FindOptions, matched, scanned int, totalSize int64, p *preparedFind, limitReached bool) error {
	if limitReached {
		if opt.JSON {
			return nil
		}
		myprint.PrintfYellow(i18n.T("\n(limit %d reached)\n", "\n（已达 %d 条上限）\n"), opt.Limit)
		return nil
	}
	if opt.JSON {
		return nil
	}
	if matched == 0 {
		// 0 匹配时给出过滤上下文, 便于判断是"没有匹配"还是"参数/阈值设置问题".
		var th []string
		if !p.older.IsZero() {
			th = append(th, fmt.Sprintf(i18n.T("modified before %s", "修改时间早于 %s"), p.older.Local().Format("2006-01-02 15:04:05")))
		}
		if !p.newer.IsZero() {
			th = append(th, fmt.Sprintf(i18n.T("modified after %s", "修改时间晚于 %s"), p.newer.Local().Format("2006-01-02 15:04:05")))
		}
		ctx := ""
		if len(th) > 0 {
			ctx = " (" + strings.Join(th, ", ") + ")"
		}
		myprint.PrintfBoldYellow(i18n.T("\nno objects matched%s out of %d scanned\n", "\n无匹配对象%s（共扫描 %d 个）\n"), ctx, scanned)
		return nil
	}
	if p.filtersActive {
		myprint.PrintfBoldBlue(i18n.T("\n%d matching objects (%s) out of %d scanned\n", "\n%d 个匹配对象（%s），共扫描 %d 个\n"), matched, FormatBytes(totalSize), scanned)
		return nil
	}
	myprint.PrintfBoldBlue(i18n.T("\n%d matching objects (%s)\n", "\n%d 个匹配对象（%s）\n"), matched, FormatBytes(totalSize))
	return nil
}

// matchFindObject 对单个对象应用全部过滤条件, 返回通过与否及对象描述.
func matchFindObject(obj s3iface.ObjectInfo, opt FindOptions, p *preparedFind, prefix string) (findMatch, bool) {

	key := obj.Key
	size := obj.Size

	if opt.Larger > 0 && size <= opt.Larger {
		return findMatch{}, false
	}
	if opt.Smaller > 0 && size >= opt.Smaller {
		return findMatch{}, false
	}
	if !p.newer.IsZero() && !obj.LastModified.After(p.newer) {
		return findMatch{}, false
	}
	if !p.older.IsZero() && !obj.LastModified.Before(p.older) {
		return findMatch{}, false
	}
	depth := keyDepth(key, prefix)
	if opt.MinDepth > 0 && depth < opt.MinDepth {
		return findMatch{}, false
	}
	if opt.MaxDepth > 0 && depth > opt.MaxDepth {
		return findMatch{}, false
	}
	if p.nameRe != nil {
		base := key
		if i := strings.LastIndex(key, "/"); i >= 0 {
			base = key[i+1:]
		}
		if !p.nameRe.MatchString(base) {
			return findMatch{}, false
		}
	}
	if p.regexRe != nil && !p.regexRe.MatchString(key) {
		return findMatch{}, false
	}
	if p.pathRe != nil && !matchDirPath(p.pathRe, key, prefix) {
		return findMatch{}, false
	}
	for _, re := range p.ignoreRes {
		if re.MatchString(key) {
			return findMatch{}, false
		}
	}
	if (len(opt.Include) > 0 || len(opt.Exclude) > 0) &&
		!matchesMirrorFilters(key, opt.Include, opt.Exclude) {
		return findMatch{}, false
	}
	if opt.StorageClass != "" && !strings.EqualFold(obj.StorageClass, opt.StorageClass) {
		return findMatch{}, false
	}
	dirMarker := strings.HasSuffix(key, "/") && size == 0
	if p.typ == "file" && dirMarker {
		return findMatch{}, false
	}
	if p.typ == "dir" && !dirMarker {
		return findMatch{}, false
	}

	return findMatch{
		key:          key,
		size:         size,
		etag:         obj.ETag,
		storageClass: obj.StorageClass,
		dirMarker:    dirMarker,
		modified:     obj.LastModified,
	}, true
}

// emitFindMatch 输出单个匹配对象 (JSON / --print / 默认表格).
func emitFindMatch(c *Action, opt FindOptions, bucket string, m findMatch, fp *findPrinter) error {
	if opt.JSON {
		typ := "file"
		switch {
		case m.deleteMark:
			typ = "delete-marker"
		case m.dirMarker:
			typ = "dir"
		}
		return printJSONLine(map[string]any{
			"path":           c.S3Path(bucket, m.key),
			"size":           m.size,
			"etag":           m.etag,
			"type":           typ,
			"storageClass":   m.storageClass,
			"lastModified":   m.modified,
			"versionId":      m.versionID,
			"isDeleteMarker": m.deleteMark,
		})
	}
	if opt.Print != "" {
		myprint.Println(formatFindPrint(opt.Print, c.S3Path(bucket, m.key), m.key, m.size, m.modified, m.etag, m.storageClass, m.versionID))
		return nil
	}
	fp.add(c, bucket, m)
	return nil
}

// normalizeFindType 归一化 --type 取值.
func normalizeFindType(t string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "", "all":
		return "", nil
	case "f", "file":
		return "file", nil
	case "d", "dir", "directory":
		return "dir", nil
	}
	return "", fmt.Errorf(i18n.T("invalid --type %q: use file or dir", "无效的 --type %q：请使用 file 或 dir"), t)
}

// parseFindSort 解析 --sort 取值: name/size/time, 前缀 "-" 表示倒序.
func parseFindSort(s string) (field string, desc bool, err error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "", false, nil
	}
	desc = strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	switch s {
	case "name", "key":
		return "name", desc, nil
	case "size":
		return "size", desc, nil
	case "time", "date", "modified":
		return "time", desc, nil
	}
	return "", false, fmt.Errorf(i18n.T("invalid --sort %q: use name, size or time (prefix with - for descending order)", "无效的 --sort %q：请使用 name、size 或 time（前缀 - 表示降序）"), s)
}

// sortFindMatches 按字段排序匹配集合 (稳定排序, 同值按 key 兜底).
func sortFindMatches(matches []findMatch, field string, desc bool) {
	sort.SliceStable(matches, func(i, j int) bool {
		less := false
		switch field {
		case "size":
			if matches[i].size != matches[j].size {
				less = matches[i].size < matches[j].size
			} else {
				less = matches[i].key < matches[j].key
			}
		case "time":
			if !matches[i].modified.Equal(matches[j].modified) {
				less = matches[i].modified.Before(matches[j].modified)
			} else {
				less = matches[i].key < matches[j].key
			}
		default: // name
			less = matches[i].key < matches[j].key
		}
		if desc {
			return !less
		}
		return less
	})
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

// formatFindPrint 按 {name}/{size}/{time}/{url}/{path}/{etag}/{storage-class}/{version-id}
// 占位符格式化输出 (--print).
func formatFindPrint(format, url, key string, size int64, t time.Time, etag, storageClass, versionID string) string {
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
	s = strings.ReplaceAll(s, "{etag}", etag)
	s = strings.ReplaceAll(s, "{storage-class}", storageClass)
	s = strings.ReplaceAll(s, "{version-id}", versionID)
	return s
}

// parseFilterTime 解析时长串 ("7d10h31s") 或绝对时间 (RFC3339 / 'YYYY-MM-DD' / 'YYYY-MM-DD HH:MM:SS').
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

// ParseDuration 解析时长串, 如 "7d10h31s" / "30m" / "2d" (d=天 h=时 m=分 s=秒).
func ParseDuration(s string) (time.Duration, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return 0, errors.New(i18n.T("empty duration", "时长为空"))
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
			return 0, fmt.Errorf(i18n.T("invalid duration %q", "无效的时长 %q"), s)
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
			return 0, fmt.Errorf(i18n.T("invalid duration unit %q in %q (use d/h/m/s)", "无效的时长单位 %q（位于 %q，请使用 d/h/m/s）"), string(c), s)
		}
		var n int64
		if _, err := fmt.Sscanf(num, "%d", &n); err != nil {
			return 0, fmt.Errorf(i18n.T("invalid duration %q", "无效的时长 %q"), s)
		}
		total += time.Duration(n) * unit
		num = ""
	}
	if num != "" {
		return 0, fmt.Errorf(i18n.T("invalid duration %q (trailing number)", "无效的时长 %q（末尾多余数字）"), s)
	}
	if total <= 0 {
		return 0, fmt.Errorf(i18n.T("duration must be positive: %q", "时长必须为正数：%q"), s)
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

// parseTime 支持 RFC3339 / "2006-01-02" / "2006-01-02 15:04:05".
// 无时区的输入按本地时区解析 (与用户直觉一致, 避免被当作 UTC 产生 8 小时偏差).
func parseTime(s string) (time.Time, error) {
	layouts := []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"}
	for _, l := range layouts {
		if t, err := time.ParseInLocation(l, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf(i18n.T("unrecognized time format %q (use duration like 7d10h31s or RFC3339/'YYYY-MM-DD')", "无法识别的时间格式 %q（请使用 7d10h31s 之类的时长或 RFC3339/'YYYY-MM-DD'）"), s)
}
