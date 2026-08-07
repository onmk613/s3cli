package cmd

import (
	"s3cli/internal/action"
	"s3cli/internal/s3path"

	"github.com/spf13/cobra"
)

func init() { Register("object", "Object Operations", NewTagCmd) }

// NewTagCmd 管理桶/对象标签
func NewTagCmd() *cobra.Command {
	tagCmd := &cobra.Command{
		Use:   "tag",
		Short: "Manage tags for buckets and objects",
	}
	tagCmd.AddCommand(NewSetTagCmd(), NewListTagCmd(), NewRemoveTagCmd())
	return tagCmd
}

// NewSetTagCmd 设置标签
func NewSetTagCmd() *cobra.Command {
	var legacy map[string]string
	cmd := &cobra.Command{
		Use:               "set [alias:bucket[/key]] ...",
		Aliases:           []string{"s"},
		Short:             "Set tag(s) on a bucket or object(s)",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: AutoCompletePath,
		Annotations:       ParseS3OnlyPath,
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.SetTag(dst.Bucket, dst.Key, legacy)
		}),
	}
	cmd.Flags().StringToStringVar(&legacy, "tag", nil, "key1=value1,key2=value2 format")
	_ = cmd.MarkFlagRequired("tag")
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
