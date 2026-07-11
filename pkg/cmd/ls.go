package cmd

import (
	"fmt"

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
		Use:               "ls [alias:[bucket/[path]]]",
		Aliases:           []string{"list", "l"},
		Short:             "List objects or buckets",
		ValidArgsFunction: AutoCompletePath,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.S3Client, opts *Context, s3path *utils.S3Path) error {
			return S3.ListObjects(s3path.Bucket, s3path.Key, opts.Global.ListAll)
		}, &opts),
	}
	cmd.Flags().BoolVarP(&opts.Global.ListAll, "all", "a", false, "Recursively list all objects in all (or specified) buckets")
	return cmd
}

func NewDuCmd() *cobra.Command {
	var blockSizeStr string
	opts := newCmdContext()
	cmd := &cobra.Command{
		Use:               "du [alias:bucket/path] ...",
		Short:             "Show disk usage of buckets or paths",
		ValidArgsFunction: AutoCompletePath,
		Args:              cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var duOpt action.DuOptions
			if blockSizeStr != "" {
				bs, err := utils.ParseBytes(blockSizeStr)
				if err != nil {
					return fmt.Errorf("--block-size: %w", err)
				}
				duOpt.BlockSize = bs
			}
			run := NewRunE(func(S3 action.S3Client, opts *Context, s3path *utils.S3Path) error {
				return S3.DuObject(duOpt, s3path.Bucket, s3path.Key)
			}, &opts)
			return run(cmd, args)
		},
	}
	cmd.Flags().StringVarP(&blockSizeStr, "block-size", "B", "", "Round each object size up to this block size (e.g. 4K, 4096) to estimate on-disk usage")
	return cmd
}

func NewInfoCmd() *cobra.Command {
	opts := newCmdContext()
	cmd := &cobra.Command{
		Use:               "info [alias:bucket[/path]] ...",
		Short:             "Show information about bucket(s) or object(s)",
		ValidArgsFunction: AutoCompletePath,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.S3Client, opts *Context, s3path *utils.S3Path) error {
			return S3.Info(s3path.Bucket, s3path.Key)
		}, &opts),
	}
	return cmd
}

func NewLsVersionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "lsv [alias:bucket[/prefix]] ...",
		Aliases:           []string{"ls-versions", "list-versions"},
		Short:             "List object versions (including delete markers)",
		ValidArgsFunction: AutoCompletePath,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.S3Client, _ *Context, s3path *utils.S3Path) error {
			return S3.ListObjectVersions(s3path.Bucket, s3path.Key)
		}, nil),
	}
}
