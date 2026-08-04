// object-list.go 实现对象列举 ListObjects (ls), 参数与 mc ls 对齐:
// --recursive/-r 递归, --versions 列版本, --incomplete/-I 列进行中分片上传,
// --summarize 汇总 (对象数/总大小). bucket 为空时列桶.

package action

import (
	"fmt"
	"strings"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/s3iface"
)

// ListOptions ls 命令参数 (mc ls 对齐).
type ListOptions struct {
	Recursive  bool // -r: 递归列举全部层级
	Versions   bool // --versions: 列出对象的所有版本与 delete-marker
	Incomplete bool // -I/--incomplete: 列出进行中的 multipart upload
	Summarize  bool // --summarize: 追加对象数与总大小汇总
	JSON       bool // --json: JSON lines 输出
}

// ListObjects 列出桶 / 对象. bucket 为空时列出当前凭证下所有桶.
func (c *Action) ListObjects(opt ListOptions, bucket, prefix string) error {
	if bucket == "" {
		buckets, err := c.S3.ListBuckets(c.Ctx)
		if err != nil {
			return fmt.Errorf("list buckets: %s", FormatAPIError(err))
		}
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
			myprint.PrintfDim("[%s]   ", bucket.CreationDate.Format("2006-01-02 15:04"))
			myprint.PrintfGreen("%s\n", c.S3Path(bucket.Name, ""))
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
	opts := &s3iface.ListObjectsV2Options{
		Prefix: prefix,
	}
	if !opt.Recursive {
		opts.Delimiter = "/"
	}

	var count int64
	var totalSize int64
	var hasOutput bool
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
			myprint.PrintfBlue("%-22s %12s   DIR   %s\n", "", "-", c.S3Path(bucket, p))
		}
		for _, item := range page.Contents {
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
				myprint.PrintfBlue("%-22s %12s   DIR   %s\n", "", "-", c.S3Path(bucket, item.Key))
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
			myprint.PrintfDim("[%s]  ", item.LastModified.Format("2006-01-02 15:04:05"))
			myprint.Printf("%12d   ", item.Size)
			myprint.PrintfGreen("FILE  %s\n", c.S3Path(bucket, item.Key))
		}
	}

	// 递归模式 (ls -r) 下如果完全没有输出，回退到非递归列举以显示一级目录。
	// 某些 S3 实现 (如 SeaweedFS) 在无 delimiter 时不返回目录标记对象，
	// 导致只有空目录的前缀递归列举结果为空。
	if opt.Recursive && !hasOutput {
		return c.listObjectsV2(bucket, prefix, ListOptions{JSON: opt.JSON, Summarize: opt.Summarize})
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
		myprint.PrintfBoldBlue("[%s] %d object(s), %s\n", c.S3Path(bucket, prefix), count, FormatBytes(totalSize))
	}
	return nil
}

// listObjectVersionsAsLs 以 ls 风格列举对象版本 (ls --versions).
func (c *Action) listObjectVersionsAsLs(bucket, prefix string, opt ListOptions) error {
	paginator := c.S3.NewListObjectVersionsPaginator(bucket,
		&s3iface.ListObjectVersionsOptions{Prefix: prefix})

	var count int64
	var totalSize int64
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(c.Ctx)
		if err != nil {
			return fmt.Errorf("list versions: %s", FormatAPIError(err))
		}
		for _, v := range page.Versions {
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
			myprint.Printf("%s ", flag)
			myprint.PrintfDim("[%s]  ", v.LastModified.Format("2006-01-02 15:04:05"))
			myprint.Printf("%12d   ", v.Size)
			myprint.PrintfGreen("%s  ", c.S3Path(bucket, v.Key))
			myprint.PrintfCyan("ID=%s\n", v.VersionID)
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
			myprint.PrintfRed("%s ", flag)
			myprint.PrintfDim("[%s]  ", m.LastModified.Format("2006-01-02 15:04:05"))
			myprint.Printf("%12s   ", "-")
			myprint.PrintfRed("%s  ", c.S3Path(bucket, m.Key))
			myprint.PrintfCyan("ID=%s\n", m.VersionID)
		}
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
		myprint.PrintfBoldBlue("[%s] %d version(s), %s\n", c.S3Path(bucket, prefix), count, FormatBytes(totalSize))
	}
	return nil
}

// listIncompleteUploads 列出进行中的分片上传 (ls --incomplete).
func (c *Action) listIncompleteUploads(bucket, prefix string, opt ListOptions) error {
	out, err := c.S3.ListMultipartUploads(c.Ctx, bucket, &s3iface.ListMultipartUploadsOptions{
		Prefix: prefix,
	})
	if err != nil {
		return fmt.Errorf("list multipart uploads: %s", FormatAPIError(err))
	}
	var count int
	for _, u := range out.Uploads {
		count++
		initiated := ""
		if !u.Initiated.IsZero() {
			initiated = u.Initiated.Format("2006-01-02 15:04:05")
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
		myprint.PrintfDim("[%s]  ", initiated)
		myprint.Printf("%12s   ", "-")
		myprint.PrintfYellow("INCOMPLETE  %s  ", c.S3Path(bucket, u.Key))
		myprint.PrintfCyan("uploadId=%s\n", u.UploadID)
	}
	if count == 0 {
		if opt.JSON {
			return nil
		}
		myprint.PrintfBoldYellow("%s: no in-progress multipart uploads\n", c.S3Path(bucket, prefix))
		return nil
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
		myprint.PrintfBoldBlue("[%s] %d in-progress upload(s)\n", c.S3Path(bucket, prefix), count)
	}
	return nil
}
