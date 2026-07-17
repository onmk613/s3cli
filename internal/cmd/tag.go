package cmd

import (
	"s3cli/internal/action"
	"s3cli/internal/utils"

	"github.com/spf13/cobra"
)

func init() { Register("object", "Object Operations", NewTagCmd) }

func NewTagCmd() *cobra.Command {
	tagCmd := &cobra.Command{
		Use:   "tag",
		Short: "Manage tags for buckets and objects",
	}
	tagCmd.AddCommand(NewSetTagCmd(), NewGetTagCmd(), NewDelTagCmd())
	return tagCmd
}

func NewSetTagCmd() *cobra.Command {
	var tagString map[string]string
	opts := newCmdContext()
	cmd := &cobra.Command{
		Use:               "set --tag k=v,k2=v2 [alias:bucket[/key]]",
		Aliases:           []string{"s"},
		Short:             "Set tag(s) on a bucket or object(s)",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: AutoCompletePath,
		RunE: NewRunE(func(S3 action.S3Client, _ *Context, s3path *utils.S3Path) error {
			return S3.SetTag(s3path.Bucket, s3path.Key, tagString)
		}, &opts),
	}
	cmd.Flags().StringToStringVar(&tagString, "tag", nil, "Comma-separated tag set, e.g. env=prod,team=infra")
	_ = cmd.MarkFlagRequired("tag")
	return cmd
}

func NewGetTagCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "get [alias:bucket[/key]] ...",
		Aliases:           []string{"ls", "list", "l"},
		Short:             "Get tag(s) of bucket(s) or object(s)",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: AutoCompletePath,
		RunE: NewRunE(func(S3 action.S3Client, _ *Context, s3path *utils.S3Path) error {
			return S3.GetTag(s3path.Bucket, s3path.Key)
		}, nil),
	}
}

func NewDelTagCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "del [alias:bucket[/key]] ...",
		Aliases:           []string{"delete", "rm", "remove", "d"},
		Short:             "Delete tag(s) from bucket(s) or object(s)",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: AutoCompletePath,
		RunE: NewRunE(func(S3 action.S3Client, _ *Context, s3path *utils.S3Path) error {
			return S3.DelTag(s3path.Bucket, s3path.Key)
		}, nil),
	}
}
