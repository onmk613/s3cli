// object-list.go 实现对象列举 ListObjects (ls), 支持:
// --recursive/-r 递归, --versions 列版本, --incomplete/-I 列进行中分片上传,
// --summarize 汇总 (对象数/总大小). bucket 为空时列桶.

package action

import (
	"fmt"
	"strings"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/i18n"
	"s3cli/pkg/s3iface"
)

// ListOptions ls 命令参数.
type ListOptions struct {
	Recursive  bool     // -r: 递归列举全部层级
	Versions   bool     // --versions: 列出对象的所有版本与 delete-marker
	Incomplete bool     // -I/--incomplete: 列出进行中的 multipart upload
	Summarize  bool     // --summarize: 追加对象数与总大小汇总
	JSON       bool     // --json: JSON lines 输出
	Include    []string // --include: 仅列出匹配任一 glob 的对象 (建议配合 -r)
	Exclude    []string // --exclude: 不列出匹配任一 glob 的对象
}

// lsRow 列举输出的表格行 (未指定 --json 时).
type lsRow struct {
	date  string
	size  string
	typ   string
	path  string
	extra string
	color myprint.Color
}

// lsTimeLayout 列举输出的统一时间格式.
const lsTimeLayout = "2006-01-02 15:04:05"

// ListObjects 列出桶 / 对象. bucket 为空时列出当前凭证下所有桶.
func (c *Action) ListObjects(opt ListOptions, bucket, prefix string) error {
	if bucket == "" {
		buckets, err := c.S3.ListBuckets(c.Ctx)
		if err != nil {
			return fmt.Errorf("list buckets: %s", FormatAPIError(err))
		}
		var rows [][2]myprint.Cell
		for _, bucket := range buckets {
			if opt.JSON {
				if err := printJSONLine(map[string]any{
					"kind":         "bucket",
					"name":         bucket.Name,
					"creationDate": bucket.CreationDate,
				}); err != nil {
					return err
				}
				continue
			}
			rows = append(rows, [2]myprint.Cell{
				{Text: bucket.CreationDate.Format("2006-01-02 15:04"), Color: myprint.Dim},
				{Text: c.S3Path(bucket.Name, ""), Color: myprint.Green},
			})
		}
		if !opt.JSON {
			tbl := myprint.NewTable(i18n.T("Created", "创建时间"), i18n.T("Bucket", "存储桶"))
			for _, r := range rows {
				tbl.AddRow(r[0], r[1])
			}
			tbl.Render()
		}
		return nil
	}

	switch {
	case opt.Incomplete:
		return c.listIncompleteUploads(bucket, prefix, opt)
	case opt.Versions:
		return c.listObjectVersionsAsLs(bucket, prefix, opt)
	default:
		return c.listObjectsV2(bucket, prefix, opt)
	}
}

// lsTableRowLimit 表格输出行数上限: 超过后放弃对齐表格,
// 改为逐行流式输出 TSV 文本 (首行表头, 制表符分隔), 避免超大列举的内存峰值与首行延迟.
const lsTableRowLimit = 1000

// lsTable ls 输出的行收集器: 行数未超上限时渲染对齐表格,
// 超过后由 Table 自动切换为流式 TSV 输出 (内存有界).
type lsTable struct {
	tbl   *myprint.Table
	extra bool // 是否有末尾附加列 (版本 ID / 上传 ID)
}

// newLsTable 构造 ls 表格收集器; extraHeader 为末尾附加列名.
func newLsTable(extraHeader string) *lsTable {
	headers := []string{
		i18n.T("Time", "时间"),
		i18n.T("Size", "大小"),
		i18n.T("Type", "类型"),
		i18n.T("Path", "路径"),
	}
	if extraHeader != "" {
		headers = append(headers, extraHeader)
	}
	return &lsTable{
		tbl:   myprint.NewTable(headers...).AlignRight(1).PlainRowLimit(lsTableRowLimit),
		extra: extraHeader != "",
	}
}

// add 追加一行 (超上限时立即写出).
func (t *lsTable) add(r lsRow) {
	cells := []myprint.Cell{
		{Text: r.date, Color: myprint.Dim},
		{Text: r.size},
		{Text: r.typ, Color: r.color},
		{Text: r.path, Color: r.color},
	}
	if t.extra {
		cells = append(cells, myprint.Cell{Text: r.extra, Color: myprint.Cyan})
	}
	t.tbl.AddRow(cells...)
}

// render 输出 (已切换流式时无动作).
func (t *lsTable) render() {
	t.tbl.Render()
}

// listObjectsV2 递归或单层列举对象.
func (c *Action) listObjectsV2(bucket, prefix string, opt ListOptions) error {
	opts := &s3iface.ListObjectsV2Options{
		Prefix: prefix,
	}
	if !opt.Recursive {
		opts.Delimiter = "/"
	}

	var count int64
	var totalSize int64
	var hasOutput bool
	tbl := newLsTable("")
	paginator := c.S3.NewListObjectsV2Paginator(bucket, opts)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(c.Ctx)
		if err != nil {
			return fmt.Errorf("list objects: %s", FormatAPIError(err))
		}
		for _, p := range page.CommonPrefixes {
			hasOutput = true
			if opt.JSON {
				if err := printJSONLine(map[string]any{
					"kind": "dir",
					"path": c.S3Path(bucket, p),
				}); err != nil {
					return err
				}
				continue
			}
			tbl.add(lsRow{date: "-", size: "-", typ: "DIR", path: c.S3Path(bucket, p), color: myprint.Blue})
		}
		for _, item := range page.Contents {
			// --include/--exclude 过滤 (对完整 key 做 glob; 建议配合 -r)
			if len(opt.Include) > 0 || len(opt.Exclude) > 0 {
				if !matchesMirrorFilters(item.Key, opt.Include, opt.Exclude) {
					hasOutput = true // 有对象但被过滤; 避免触发空结果回退
					continue
				}
			}
			hasOutput = true
			// 目录标记对象 (以 "/" 结尾且 0 字节) 显示为 DIR
			if strings.HasSuffix(item.Key, "/") && item.Size == 0 {
				if opt.JSON {
					if err := printJSONLine(map[string]any{
						"kind": "dir",
						"path": c.S3Path(bucket, item.Key),
					}); err != nil {
						return err
					}
					continue
				}
				tbl.add(lsRow{date: "-", size: "-", typ: "DIR", path: c.S3Path(bucket, item.Key), color: myprint.Blue})
				continue
			}
			count++
			totalSize += item.Size
			if opt.JSON {
				if err := printJSONLine(map[string]any{
					"kind":         "file",
					"path":         c.S3Path(bucket, item.Key),
					"size":         item.Size,
					"lastModified": item.LastModified,
				}); err != nil {
					return err
				}
				continue
			}
			tbl.add(lsRow{
				date:  item.LastModified.Format(lsTimeLayout),
				size:  fmt.Sprintf("%d", item.Size),
				typ:   "FILE",
				path:  c.S3Path(bucket, item.Key),
				color: myprint.Green,
			})
		}
	}

	// 递归模式 (ls -r) 下如果完全没有输出，回退到非递归列举以显示一级目录。
	// 某些 S3 实现 (如 SeaweedFS) 在无 delimiter 时不返回目录标记对象，
	// 导致只有空目录的前缀递归列举结果为空。
	if opt.Recursive && !hasOutput {
		return c.listObjectsV2(bucket, prefix, ListOptions{JSON: opt.JSON, Summarize: opt.Summarize})
	}
	if !opt.JSON {
		tbl.render()
	}
	if opt.Summarize {
		if opt.JSON {
			if err := printJSONLine(map[string]any{
				"kind":      "summary",
				"path":      c.S3Path(bucket, prefix),
				"count":     count,
				"totalSize": totalSize,
			}); err != nil {
				return err
			}
			return nil
		}
		myprint.PrintfBoldBlue(i18n.T("[%s] %d object(s), %s\n", "[%s] %d 个对象，%s\n"), c.S3Path(bucket, prefix), count, FormatBytes(totalSize))
	}
	return nil
}

// listObjectVersionsAsLs 以 ls 风格列举对象版本 (ls --versions).
func (c *Action) listObjectVersionsAsLs(bucket, prefix string, opt ListOptions) error {
	paginator := c.S3.NewListObjectVersionsPaginator(bucket,
		&s3iface.ListObjectVersionsOptions{Prefix: prefix})

	var count int64
	var totalSize int64
	tbl := newLsTable(i18n.T("Version ID", "版本ID"))
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(c.Ctx)
		if err != nil {
			return fmt.Errorf("list versions: %s", FormatAPIError(err))
		}
		for _, v := range page.Versions {
			if len(opt.Include) > 0 || len(opt.Exclude) > 0 {
				if !matchesMirrorFilters(v.Key, opt.Include, opt.Exclude) {
					continue
				}
			}
			count++
			totalSize += v.Size
			if opt.JSON {
				if err := printJSONLine(map[string]any{
					"kind":         "version",
					"path":         c.S3Path(bucket, v.Key),
					"size":         v.Size,
					"lastModified": v.LastModified,
					"versionId":    v.VersionID,
					"isLatest":     v.IsLatest,
				}); err != nil {
					return err
				}
				continue
			}
			flag := "VER "
			if v.IsLatest {
				flag = "VER*"
			}
			tbl.add(lsRow{
				date:  v.LastModified.Format(lsTimeLayout),
				size:  fmt.Sprintf("%d", v.Size),
				typ:   flag,
				path:  c.S3Path(bucket, v.Key),
				extra: v.VersionID,
				color: myprint.Green,
			})
		}
		for _, m := range page.DeleteMarkers {
			if opt.JSON {
				if err := printJSONLine(map[string]any{
					"kind":         "delete-marker",
					"path":         c.S3Path(bucket, m.Key),
					"lastModified": m.LastModified,
					"versionId":    m.VersionID,
					"isLatest":     m.IsLatest,
				}); err != nil {
					return err
				}
				continue
			}
			flag := "DEL "
			if m.IsLatest {
				flag = "DEL*"
			}
			tbl.add(lsRow{
				date:  m.LastModified.Format(lsTimeLayout),
				size:  "-",
				typ:   flag,
				path:  c.S3Path(bucket, m.Key),
				extra: m.VersionID,
				color: myprint.Red,
			})
		}
	}
	if !opt.JSON {
		tbl.render()
	}
	if opt.Summarize {
		if opt.JSON {
			if err := printJSONLine(map[string]any{
				"kind":      "summary",
				"path":      c.S3Path(bucket, prefix),
				"count":     count,
				"totalSize": totalSize,
			}); err != nil {
				return err
			}
			return nil
		}
		myprint.PrintfBoldBlue(i18n.T("[%s] %d version(s), %s\n", "[%s] %d 个版本，%s\n"), c.S3Path(bucket, prefix), count, FormatBytes(totalSize))
	}
	return nil
}

// listIncompleteUploads 列出进行中的分片上传 (ls --incomplete).
func (c *Action) listIncompleteUploads(bucket, prefix string, opt ListOptions) error {
	// 翻页列举 (与 mpu list 一致): ListMultipartUploads 单页上限 1000,
	// 不翻页会静默截断且与 `mpu list` 输出不一致。
	uploads, err := c.listAllMultipartUploads(c.Ctx, bucket, prefix)
	if err != nil {
		return fmt.Errorf("list multipart uploads: %s", FormatAPIError(err))
	}
	var count int
	tbl := newLsTable(i18n.T("Upload ID", "上传ID"))
	for _, u := range uploads {
		count++
		initiated := ""
		if !u.Initiated.IsZero() {
			initiated = u.Initiated.Format(lsTimeLayout)
		}
		if opt.JSON {
			if err := printJSONLine(map[string]any{
				"kind":      "incomplete",
				"path":      c.S3Path(bucket, u.Key),
				"initiated": initiated,
				"uploadId":  u.UploadID,
			}); err != nil {
				return err
			}
			continue
		}
		tbl.add(lsRow{
			date:  initiated,
			size:  "-",
			typ:   "INCOMPLETE",
			path:  c.S3Path(bucket, u.Key),
			extra: u.UploadID,
			color: myprint.Yellow,
		})
	}
	if count == 0 {
		if opt.JSON {
			return nil
		}
		myprint.PrintfBoldYellow(i18n.T("%s: no in-progress multipart uploads\n", "%s：没有进行中的分段上传\n"), c.S3Path(bucket, prefix))
		return nil
	}
	if !opt.JSON {
		tbl.render()
	}
	if opt.Summarize {
		if opt.JSON {
			if err := printJSONLine(map[string]any{
				"kind":  "summary",
				"path":  c.S3Path(bucket, prefix),
				"count": count,
			}); err != nil {
				return err
			}
			return nil
		}
		myprint.PrintfBoldBlue(i18n.T("[%s] %d in-progress upload(s)\n", "[%s] %d 个进行中的上传\n"), c.S3Path(bucket, prefix), count)
	}
	return nil
}
