package cmd

import (
	"s3cli/pkg/action"
	"s3cli/pkg/utils"

	"github.com/spf13/cobra"
)

func init() {
	Register("read", "Read Commands", NewLsCmd)
	Register("read", "Read Commands", NewDuCmd)
	Register("read", "Read Commands", NewInfoCmd)
	Register("read", "Read Commands", NewLsVersionsCmd)
}

func NewLsCmd() *cobra.Command {
	opts := newCmdContext()
	opts.Global.AllowAliasOnly = true // ls 支持只输入 alias 来列出所有 bucket
	cmd := &cobra.Command{
		Use:     "ls [alias:[bucket/[path]]]",
		Aliases: []string{"list", "l"},
		Short:   "List objects or buckets",
		Args:    cobra.MinimumNArgs(1),
		RunE: NewRunE(ActionFunc(func(S3 action.S3Client, opts *CmdContext, s3path *utils.S3Path) error {
			return S3.ListObjects(s3path.Bucket, s3path.Key, opts.Global.ListAll)
		}), &opts),
	}
	cmd.Flags().BoolVarP(&opts.Global.ListAll, "all", "a", false, "Recursively list all objects in all (or specified) buckets")
	return cmd
}

func NewDuCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "du [alias:bucket/path] ...",
		Short: "Show disk usage of buckets or paths",
		Args:  cobra.MinimumNArgs(1),
		RunE: NewRunE(ActionFunc(func(S3 action.S3Client, opts *CmdContext, s3path *utils.S3Path) error {
			return S3.DuObject(s3path.Bucket, s3path.Key)
		}), nil),
	}
}

func NewInfoCmd() *cobra.Command {
	opts := newCmdContext()
	cmd := &cobra.Command{
		Use:   "info [alias:bucket[/path]] ...",
		Short: "Show information about a bucket or object",
		Args:  cobra.MinimumNArgs(1),
		RunE: NewRunE(ActionFunc(func(S3 action.S3Client, opts *CmdContext, s3path *utils.S3Path) error {
			return S3.Info(s3path.Bucket, s3path.Key)
		}), &opts),
	}
	return cmd
}

func NewLsVersionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "lsv [alias:bucket[/prefix]] ...",
		Aliases: []string{"ls-versions", "list-versions"},
		Short:   "List object versions (including delete markers)",
		Args:    cobra.MinimumNArgs(1),
		RunE: NewRunE(ActionFunc(func(S3 action.S3Client, _ *CmdContext, s3path *utils.S3Path) error {
			return S3.ListOjbectVersions(s3path.Bucket, s3path.Key)
		}), nil),
	}
}
