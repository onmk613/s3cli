// object-list.go 实现对象列举 ListObjects (ls), 支持:
// --recursive/-r 递归, --versions 列版本, --incomplete/-I 列进行中分片上传,
// --summarize 汇总 (对象数/总大小). bucket 为空时列桶.

package action

import (
	"fmt"
	"strings"

	"s3cli/internal/action/render"
	"s3cli/internal/api"
	myprint "s3cli/internal/fmtutil"
	"s3cli/internal/i18n"
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

// ListObjects 列出桶 / 对象. bucket 为空时列出当前凭证下所有桶.
func (c *Action) ListObjects(opt ListOptions, bucket, prefix string) error {
	if bucket == "" {
		buckets, err := c.S3.ListBuckets(c.Ctx)
		if err != nil {
			return fmt.Errorf("list buckets: %w", err)
		}
		var rows [][2]myprint.Cell
		for _, bucket := range buckets {
			if opt.JSON {
				if err := render.JSONLine(map[string]any{
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

// listObjectsV2 递归或单层列举对象.
func (c *Action) listObjectsV2(bucket, prefix string, opt ListOptions) error {
	opts := &api.ListObjectsV2Options{
		Prefix: prefix,
	}
	if !opt.Recursive {
		opts.Delimiter = "/"
	}

	var count int64
	var totalSize int64
	var hasOutput bool
	tbl := render.NewLsTable("")
	err := c.forEachObjectPage(c.Ctx, bucket, opts, func(page *api.ListObjectsV2Output) error {
		for _, p := range page.CommonPrefixes {
			hasOutput = true
			if opt.JSON {
				if err := render.JSONLine(map[string]any{
					"kind": "dir",
					"path": c.S3Path(bucket, p),
				}); err != nil {
					return err
				}
				continue
			}
			tbl.Add(render.LsRow{Size: "-", Type: "DIR", Path: c.S3Path(bucket, p), Color: myprint.Blue})
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
					if err := render.JSONLine(map[string]any{
						"kind": "dir",
						"path": c.S3Path(bucket, item.Key),
					}); err != nil {
						return err
					}
					continue
				}
				tbl.Add(render.LsRow{Size: "-", Type: "DIR", Path: c.S3Path(bucket, item.Key), Color: myprint.Blue})
				continue
			}
			count++
			totalSize += item.Size
			if opt.JSON {
				if err := render.JSONLine(map[string]any{
					"kind":         "file",
					"path":         c.S3Path(bucket, item.Key),
					"size":         item.Size,
					"lastModified": item.LastModified,
				}); err != nil {
					return err
				}
				continue
			}
			tbl.Add(render.LsRow{
				Time:  item.LastModified,
				Size:  fmt.Sprintf("%d", item.Size),
				Type:  "FILE",
				Path:  c.S3Path(bucket, item.Key),
				Color: myprint.Green,
			})
		}
		return nil
	})
	if err != nil {
		return err
	}

	// 递归模式 (ls -r) 下如果完全没有输出，回退到非递归列举以显示一级目录。
	// 某些 S3 实现 (如 SeaweedFS) 在无 delimiter 时不返回目录标记对象，
	// 导致只有空目录的前缀递归列举结果为空。
	if opt.Recursive && !hasOutput {
		return c.listObjectsV2(bucket, prefix, ListOptions{JSON: opt.JSON, Summarize: opt.Summarize})
	}
	if !opt.JSON {
		tbl.Render()
	}
	if opt.Summarize {
		if opt.JSON {
			if err := render.JSONLine(map[string]any{
				"kind":      "summary",
				"path":      c.S3Path(bucket, prefix),
				"count":     count,
				"totalSize": totalSize,
			}); err != nil {
				return err
			}
			return nil
		}
		myprint.PrintfBoldBlue(i18n.T("[%s] %d object(s), %s\n", "[%s] %d 个对象，%s\n"), c.S3Path(bucket, prefix), count, myprint.FormatBytes(totalSize))
	}
	return nil
}

// listObjectVersionsAsLs 以 ls 风格列举对象版本 (ls --versions).
//
// 分页与 Version/DeleteMarker 的归一由 forEachVersion 提供, 这里只负责
// 过滤、计数与逐条渲染。
func (c *Action) listObjectVersionsAsLs(bucket, prefix string, opt ListOptions) error {
	var count int64
	var totalSize int64
	tbl := render.NewLsTable(i18n.T("Version ID", "版本ID"))

	err := c.forEachVersion(c.Ctx, bucket, prefix, func(v VersionEntry) error {
		if len(opt.Include) > 0 || len(opt.Exclude) > 0 {
			if !matchesMirrorFilters(v.Key, opt.Include, opt.Exclude) {
				return nil
			}
		}
		if !v.IsDeleteMarker {
			count++
			totalSize += v.Size
		}
		if opt.JSON {
			kind := "version"
			rec := map[string]any{
				"kind":         kind,
				"path":         c.S3Path(bucket, v.Key),
				"lastModified": v.LastModified,
				"versionId":    v.VersionID,
				"isLatest":     v.IsLatest,
			}
			if v.IsDeleteMarker {
				rec["kind"] = "delete-marker"
			} else {
				rec["size"] = v.Size
			}
			return render.JSONLine(rec)
		}
		row := render.LsRow{
			Time:  v.LastModified,
			Size:  fmt.Sprintf("%d", v.Size),
			Type:  render.VersionFlag(v.IsDeleteMarker, v.IsLatest),
			Path:  c.S3Path(bucket, v.Key),
			Extra: v.VersionID,
			Color: render.VersionColor(v.IsDeleteMarker),
		}
		if v.IsDeleteMarker {
			row.Size = "-"
		}
		tbl.Add(row)
		return nil
	})
	if err != nil {
		return err
	}

	if !opt.JSON {
		tbl.Render()
	}
	if opt.Summarize {
		if opt.JSON {
			if err := render.JSONLine(map[string]any{
				"kind":      "summary",
				"path":      c.S3Path(bucket, prefix),
				"count":     count,
				"totalSize": totalSize,
			}); err != nil {
				return err
			}
			return nil
		}
		myprint.PrintfBoldBlue(i18n.T("[%s] %d version(s), %s\n", "[%s] %d 个版本，%s\n"), c.S3Path(bucket, prefix), count, myprint.FormatBytes(totalSize))
	}
	return nil
}

// listIncompleteUploads 列出进行中的分片上传 (ls --incomplete).
func (c *Action) listIncompleteUploads(bucket, prefix string, opt ListOptions) error {
	// 翻页列举 (与 mpu list 一致): ListMultipartUploads 单页上限 1000,
	// 不翻页会静默截断且与 `mpu list` 输出不一致。
	uploads, err := c.listAllMultipartUploads(c.Ctx, bucket, prefix)
	if err != nil {
		return fmt.Errorf("list multipart uploads: %w", err)
	}
	var count int
	tbl := render.NewLsTable(i18n.T("Upload ID", "上传ID"))
	for _, u := range uploads {
		count++
		initiated := u.Initiated
		if opt.JSON {
			if err := render.JSONLine(map[string]any{
				"kind":      "incomplete",
				"path":      c.S3Path(bucket, u.Key),
				"initiated": initiated,
				"uploadId":  u.UploadID,
			}); err != nil {
				return err
			}
			continue
		}
		tbl.Add(render.LsRow{
			Time:  initiated,
			Size:  "-",
			Type:  "INCOMPLETE",
			Path:  c.S3Path(bucket, u.Key),
			Extra: u.UploadID,
			Color: myprint.Yellow,
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
		tbl.Render()
	}
	if opt.Summarize {
		if opt.JSON {
			if err := render.JSONLine(map[string]any{
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
