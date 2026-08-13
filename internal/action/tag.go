// tag.go 实现桶/对象标签管理: Set/Get/DelTag, prefix 为空时操作桶标签, 否则操作对象标签.

package action

import (
	"fmt"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/i18n"
	"s3cli/pkg/s3iface"
)

// SetTag 设置桶或对象标签; prefix 为空时操作桶标签, 否则操作对象标签.
func (c *Action) SetTag(bucket, prefix string, tagStr map[string]string) error {
	tags := parseTagPairs(tagStr)
	if prefix == "" {
		if err := c.S3.SetBucketTagging(c.Ctx, bucket, tags); err != nil {
			return fmt.Errorf("set bucket tag: %s", FormatAPIError(err))
		}
		myprint.PrintfBoldGreen(i18n.T("Tag set for %s (%d tags)\n", "已为 %s 设置标签（%d 个）\n"), c.S3Path(bucket, prefix), len(tags))
		return nil
	}

	if err := c.S3.SetObjectTagging(c.Ctx, bucket, prefix, tags, ""); err != nil {
		return fmt.Errorf("set object tag: %s", FormatAPIError(err))
	}

	myprint.PrintfBoldGreen(i18n.T("Tag set for %s (%d tags)\n", "已为 %s 设置标签（%d 个）\n"), c.S3Path(bucket, prefix), len(tags))
	return nil
}

// TagOptions tag 命令参数.
type TagOptions struct {
	JSON bool // --json: 输出 JSON
}

// GetTag 查询并打印桶或对象的标签集合.
func (c *Action) GetTag(opt TagOptions, bucket, prefix string) error {
	var tags []s3iface.Tagging
	if prefix == "" {
		result, err := c.S3.GetBucketTagging(c.Ctx, bucket)
		if err != nil {
			return fmt.Errorf("get bucket tag: %s", FormatAPIError(err))
		}
		tags = result
	} else {
		result, err := c.S3.GetObjectTagging(c.Ctx, bucket, prefix, "")
		if err != nil {
			return fmt.Errorf("get object tag: %s", FormatAPIError(err))
		}
		tags = result
	}
	if opt.JSON {
		tagList := make([]map[string]string, 0, len(tags))
		for _, t := range tags {
			tagList = append(tagList, map[string]string{"key": t.Key, "value": t.Value})
		}
		return printJSONLine(map[string]any{
			"path": c.S3Path(bucket, prefix),
			"tags": tagList,
		})
	}
	if len(tags) == 0 {
		myprint.PrintfCyan(i18n.T("# %s: no tags\n", "# %s：无标签\n"), c.S3Path(bucket, prefix))
		return nil
	}

	myprint.PrintfBoldBlue(i18n.T("# %s tags:\n", "# %s 标签：\n"), c.S3Path(bucket, prefix))
	tbl := myprint.NewTable(i18n.T("Key", "键"), i18n.T("Value", "值"))
	for _, t := range tags {
		tbl.AddRow(
			myprint.Cell{Text: t.Key, Color: myprint.Green},
			myprint.Cell{Text: t.Value, Color: myprint.Green},
		)
	}
	tbl.Render()
	return nil
}

// DelTag 删除桶或对象的标签集合.
func (c *Action) DelTag(bucket, prefix string) error {
	if prefix == "" {
		if err := c.S3.DeleteBucketTagging(c.Ctx, bucket); err != nil {
			return fmt.Errorf("delete bucket tag: %s", FormatAPIError(err))
		}
	} else {
		if err := c.S3.DeleteObjectTagging(c.Ctx, bucket, prefix, ""); err != nil {
			return fmt.Errorf("delete object tag: %s", FormatAPIError(err))
		}
	}

	myprint.PrintfBoldGreen(i18n.T("Tags deleted for %s\n", "已删除 %s 的标签\n"), c.S3Path(bucket, prefix))
	return nil
}

func parseTagPairs(m map[string]string) []s3iface.Tagging {
	tags := make([]s3iface.Tagging, 0, len(m))
	for k, v := range m {
		tags = append(tags, s3iface.Tagging{
			Key:   k,
			Value: v,
		})
	}
	return tags
}
