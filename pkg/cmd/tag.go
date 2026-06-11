package cmd

import (
	"s3cli/pkg/action"
	"s3cli/pkg/utils"

	"github.com/spf13/cobra"
)

func init() { Register("object", "Object Operations", NewTagCmd) }

func NewTagCmd() *cobra.Command {
	tagCmd := &cobra.Command{
		Use:   "tag",
		Short: "manage tags for bucket and object(s)",
	}
	tagCmd.AddCommand(NewSetTagCmd(), NewGetTagCmd(), NewDelTagCmd())
	return tagCmd
}

func NewSetTagCmd() *cobra.Command {
	var tagString map[string]string
	opts := newCmdContext()
	cmd := &cobra.Command{
		Use:     "set --tag k=v,k2=v2 [alias:bucket[/key]]",
		Aliases: []string{"s"},
		Short:   "Set tag(s) on a bucket or object",
		Args:    cobra.MinimumNArgs(1),
		RunE: NewRunE(ActionFunc(func(S3 action.S3Client, _ *CmdContext, s3path *utils.S3Path) error {
			return S3.SetTag(s3path.Bucket, s3path.Key, tagString)
		}), &opts),
	}
	cmd.Flags().StringToStringVar(&tagString, "tag", nil, "Comma-separated tag set, e.g. env=prod,team=infra")
	_ = cmd.MarkFlagRequired("tag")
	return cmd
}

func NewGetTagCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "get [alias:bucket[/key]] ...",
		Aliases: []string{"ls", "list", "l"},
		Short:   "Get tag(s) of bucket(s) or object(s)",
		Args:    cobra.MinimumNArgs(1),
		RunE: NewRunE(ActionFunc(func(S3 action.S3Client, _ *CmdContext, s3path *utils.S3Path) error {
			return S3.GetTag(s3path.Bucket, s3path.Key)
		}), nil),
	}
}

func NewDelTagCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "del [alias:bucket[/key]] ...",
		Aliases: []string{"delete", "rm", "remove", "d"},
		Short:   "Delete tags from bucket(s) or object(s)",
		Args:    cobra.MinimumNArgs(1),
		RunE: NewRunE(ActionFunc(func(S3 action.S3Client, _ *CmdContext, s3path *utils.S3Path) error {
			return S3.DelTag(s3path.Bucket, s3path.Key)
		}), nil),
	}
}
