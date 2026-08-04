package cmd

import (
	"fmt"
	"s3cli/internal/action"
	"s3cli/internal/s3path"

	"github.com/spf13/cobra"
)

func init() {
	Register("read", "Read Commands", NewLsCmd)
	Register("read", "Read Commands", NewDuCmd)
	Register("read", "Read Commands", NewStatCmd)
	Register("read", "Read Commands", NewInfoCmd)
	Register("read", "Read Commands", NewLsVersionsCmd)
}

// NewLsCmd 列出桶/对象 (mc ls 对齐: -r/--recursive, --versions, --incomplete/-I, --summarize).
func NewLsCmd() *cobra.Command {
	var opt action.ListOptions
	AllowAliasOnly = true
	cmd := &cobra.Command{
		Use:               "ls [alias:[bucket/[path]]]",
		Aliases:           []string{"list", "l"},
		Short:             "List objects or bucket",
		ValidArgsFunction: AutoCompletePath,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.ListObjects(opt, dst.Bucket, dst.Key)
		}),
	}
	f := cmd.Flags()
	f.BoolVar(&opt.Versions, "versions", false, "List all versions of objects (including delete markers)")
	f.BoolVarP(&opt.Incomplete, "incomplete", "I", false, "List in-progress multipart uploads")
	f.BoolVar(&opt.Summarize, "summarize", false, "Print summary (number of objects, total size)")
	f.BoolVarP(&opt.Recursive, "all", "a", false, "DEPRECATED: use -r/--recursive")
	_ = cmd.Flags().MarkDeprecated("all", "use -r/--recursive instead")
	f.BoolVar(&opt.JSON, "json", false, "Output format: text or json (supported commands emit structured results)")
	return cmd
}

// NewDuCmd 磁盘占用统计 (mc du 对齐: -r/--recursive, -d/--depth; 兼容 --block-size).
func NewDuCmd() *cobra.Command {
	var blockSizeStr string
	var opt action.DuOptions
	cmd := &cobra.Command{
		Use:               "du [alias:bucket/path] ...",
		Short:             "Show disk usage of bucket or paths",
		ValidArgsFunction: AutoCompletePath,
		Args:              cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if blockSizeStr != "" {
				bs, err := action.ParseByteSize(blockSizeStr)
				if err != nil {
					return fmt.Errorf("--block-size: %w", err)
				}
				opt.BlockSize = bs
			}
			run := NewRunE(func(S3 action.Action, dst *s3path.Path) error {
				return S3.DuObject(opt, dst.Bucket, dst.Key)
			})
			return run(cmd, args)
		},
	}
	f := cmd.Flags()
	f.StringVarP(&blockSizeStr, "block-size", "B", "", "Round each object size up to this block size (e.g. 4K, 4096) to estimate on-disk usage")
	f.BoolVarP(&opt.Recursive, "recursive", "r", false, "Print the total for each directory prefix")
	f.IntVarP(&opt.Depth, "depth", "d", 0, "Print totals only for prefixes N or fewer levels below the argument (with -r)")
	f.BoolVar(&opt.JSON, "json", false, "Output format: text or json (supported commands emit structured results)")
	return cmd
}

// NewStatCmd 展示对象/桶元信息 (mc stat 对齐: -r/--recursive, --version-id/--vid, --json).
func NewStatCmd() *cobra.Command {
	var opt action.StatOptions
	cmd := &cobra.Command{
		Use:               "stat [alias:bucket[/path]] ...",
		Short:             "Show metadata about bucket or object (mc stat compatible)",
		ValidArgsFunction: AutoCompletePath,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.StatObjects(opt, dst.Bucket, dst.Key)
		}),
	}
	f := cmd.Flags()
	f.BoolVarP(&opt.Recursive, "recursive", "r", false, "Stat all objects recursively under the prefix")
	f.StringVar(&opt.VersionID, "version-id", "", "Stat a specific object version")
	f.StringVar(&opt.VersionID, "vid", "", "Alias of --version-id")
	f.BoolVar(&opt.JSON, "json", false, "Output format: text or json (supported commands emit structured results)")
	return cmd
}

// NewInfoCmd 查看元信息 (JSON 输出, 兼容旧用法).
func NewInfoCmd() *cobra.Command {
	var opt action.InfoOptions
	cmd := &cobra.Command{
		Use:               "info [alias:bucket[/path]] ...",
		Short:             "Show object/bucket metadata as JSON",
		ValidArgsFunction: AutoCompletePath,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.Info(opt, dst.Bucket, dst.Key)
		}),
	}
	f := cmd.Flags()
	f.BoolVarP(&opt.Recursive, "recursive", "r", false, "Show all objects recursively under the prefix")
	f.StringVar(&opt.VersionID, "version-id", "", "Show a specific object version")
	f.StringVar(&opt.VersionID, "vid", "", "Alias of --version-id")
	return cmd
}

// NewLsVersionsCmd 列出对象版本 (mc ls --versions 的独立命令形态, 兼容旧用法).
func NewLsVersionsCmd() *cobra.Command {
	var opt action.ListOptions
	cmd := &cobra.Command{
		Use:               "lsv [alias:bucket[/prefix]] ...",
		Aliases:           []string{"ls-versions", "list-versions"},
		Short:             "List object versions (including delete markers)",
		ValidArgsFunction: AutoCompletePath,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			opt.Versions = true
			return S3.ListObjects(opt, dst.Bucket, dst.Key)
		}),
	}
	cmd.Flags().BoolVar(&opt.Summarize, "summarize", false, "Print summary (number of versions, total size)")
	cmd.Flags().BoolVar(&opt.JSON, "json", false, "Output format: text or json (supported commands emit structured results)")
	return cmd
}
