package cmd

import (
	"fmt"
	"strings"

	"s3cli/internal/action"
	"s3cli/internal/s3path"

	"github.com/spf13/cobra"
)

func init() { Register("object", "Object Operations", NewTagCmd) }

// NewTagCmd 管理桶/对象标签 (mc tag 对齐: set/list/remove).
func NewTagCmd() *cobra.Command {
	tagCmd := &cobra.Command{
		Use:   "tag",
		Short: "Manage tags for buckets and objects",
	}
	tagCmd.AddCommand(NewSetTagCmd(), NewListTagCmd(), NewRemoveTagCmd())
	return tagCmd
}

// NewSetTagCmd 设置标签 (mc tag set --tags 'k1=v1&k2=v2' 格式; 兼容旧 --tag 逗号格式).
func NewSetTagCmd() *cobra.Command {
	var tagString string
	var legacy map[string]string
	cmd := &cobra.Command{
		Use:               "set [alias:bucket[/key]] ...",
		Aliases:           []string{"s"},
		Short:             "Set tag(s) on a bucket or object(s)",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: AutoCompletePath,
		Annotations:       ParseS3OnlyPath,
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			m, err := parseTagsString(tagString)
			if err != nil {
				return err
			}
			if len(legacy) > 0 {
				m = legacy
			}
			return S3.SetTag(dst.Bucket, dst.Key, m)
		}),
	}
	cmd.Flags().StringVar(&tagString, "tags", "", "Tag set: '<key1>=<value1>&<key2>=<value2>'")
	cmd.Flags().StringToStringVar(&legacy, "tag", nil, "DEPRECATED: use --tags")
	_ = cmd.Flags().MarkDeprecated("tag", "use --tags 'k=v&k2=v2' instead")
	return cmd
}

// NewListTagCmd 查看标签 (mc tag list; 兼容旧名 get).
func NewListTagCmd() *cobra.Command {
	var opt action.TagOptions
	cmd := &cobra.Command{
		Use:               "list [alias:bucket[/key]] ...",
		Aliases:           []string{"get", "ls", "l"},
		Short:             "List tag(s) of bucket(s) or object(s)",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: AutoCompletePath,
		Annotations:       ParseS3OnlyPath,
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.GetTag(opt, dst.Bucket, dst.Key)
		}),
	}
	cmd.Flags().BoolVar(&opt.JSON, "json", false, "Output format: text or json (supported commands emit structured results)")
	return cmd
}

// NewRemoveTagCmd 删除标签 (mc tag remove; 兼容旧名 del).
func NewRemoveTagCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "remove [alias:bucket[/key]] ...",
		Aliases:           []string{"del", "delete", "rm", "d"},
		Short:             "Remove tag(s) from bucket(s) or object(s)",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: AutoCompletePath,
		Annotations:       ParseS3OnlyPath,
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.DelTag(dst.Bucket, dst.Key)
		}),
	}
}

// parseTagsString 解析 mc 风格 'k1=v1&k2=v2' 标签串; 同时兼容旧逗号分隔格式.
func parseTagsString(s string) (map[string]string, error) {
	m := map[string]string{}
	s = strings.TrimSpace(s)
	if s == "" {
		return m, nil
	}
	sep := "&"
	if !strings.Contains(s, "&") && strings.Contains(s, ",") {
		sep = ","
	}
	for _, part := range strings.Split(s, sep) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 || strings.TrimSpace(kv[0]) == "" {
			return nil, fmt.Errorf("invalid tag %q: expected key=value", part)
		}
		m[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
	}
	return m, nil
}
