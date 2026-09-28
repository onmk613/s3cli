package render

import (
	"time"

	"s3cli/internal/fmtutil"
	"s3cli/internal/i18n"
)

// LsTableRowLimit 是表格输出的行数上限: 超过后放弃对齐表格, 改为逐行流式
// 输出 TSV 文本 (首行表头, 制表符分隔), 避免超大列举的内存峰值与首行延迟。
const LsTableRowLimit = 1000

// LsRow 是列举输出的一行。字段全部为已渲染好的字符串, 渲染层不关心它们的来源。
type LsRow struct {
	Time  time.Time
	Size  string
	Type  string
	Path  string
	Extra string // 末尾附加列 (版本 ID / 上传 ID), 为空表示无该列
	Color fmtutil.Color
}

// LsTable 是列举输出的行收集器: 行数未超上限时渲染对齐表格,
// 超过后由 Table 自动切换为流式 TSV 输出 (内存有界)。
type LsTable struct {
	tbl   *fmtutil.Table
	extra bool
}

// NewLsTable 构造列举表格; extraHeader 非空时追加一列 (如 "Version ID")。
func NewLsTable(extraHeader string) *LsTable {
	headers := []string{
		i18n.T("Time", "时间"),
		i18n.T("Size", "大小"),
		i18n.T("Type", "类型"),
		i18n.T("Path", "路径"),
	}
	if extraHeader != "" {
		headers = append(headers, extraHeader)
	}
	return &LsTable{
		tbl:   fmtutil.NewTable(headers...).AlignRight(1).PlainRowLimit(LsTableRowLimit),
		extra: extraHeader != "",
	}
}

// Add 追加一行 (超上限时立即写出)。
func (t *LsTable) Add(r LsRow) {
	cells := []fmtutil.Cell{
		{Text: formatTime(r.Time), Color: fmtutil.Dim},
		{Text: r.Size},
		{Text: r.Type, Color: r.Color},
		{Text: r.Path, Color: r.Color},
	}
	if t.extra {
		cells = append(cells, fmtutil.Cell{Text: r.Extra, Color: fmtutil.Cyan})
	}
	t.tbl.AddRow(cells...)
}

// Render 输出表格 (已切换流式时无动作)。
func (t *LsTable) Render() { t.tbl.Render() }

// VersionFlag 返回版本行前缀标记: "VER "/"VER*" 表示对象版本,
// "DEL "/"DEL*" 表示删除标记, 星号代表该版本为当前版本。
func VersionFlag(isDeleteMarker, isLatest bool) string {
	base, star := "VER ", "VER*"
	if isDeleteMarker {
		base, star = "DEL ", "DEL*"
	}
	if isLatest {
		return star
	}
	return base
}

// VersionColor 返回版本行的着色 (删除标记用红色)。
func VersionColor(isDeleteMarker bool) fmtutil.Color {
	if isDeleteMarker {
		return fmtutil.Red
	}
	return fmtutil.Green
}

// formatTime 渲染行内时间; 零值输出 "-" 而不是公元 1 年。
func formatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format(TimeLayout)
}
