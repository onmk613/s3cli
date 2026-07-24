package cmd

import (
	"s3cli/internal/action"
	"s3cli/internal/s3path"

	"github.com/spf13/cobra"
)

func init() {
	Register("read", "Read Commands", NewFindCmd)
	Register("read", "Read Commands", NewTreeCmd)
}

func NewFindCmd() *cobra.Command {
	var findOpt action.FindOptions
	opts := newCmdContext()
	cmd := &cobra.Command{
		Use:               "find [alias:bucket[/prefix]] ...",
		Short:             "Search objects by name pattern, size and modification time",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: AutoCompletePath,
		RunE: NewRunE(func(S3 action.S3Client, _ *Context, s3path *s3path.Path) error {
			return S3.FindObjects(findOpt, s3path.Bucket, s3path.Key)
		}, &opts),
	}
	cmd.Flags().StringVar(&findOpt.Name, "name", "", "Match object basename (shell glob, e.g. '*.log')")
	cmd.Flags().BoolVar(&findOpt.NameRegex, "regex", false, "Treat --name as RE2 regular expression")
	cmd.Flags().Int64Var(&findOpt.MinSize, "min-size", 0, "Minimum object size in bytes (inclusive)")
	cmd.Flags().Int64Var(&findOpt.MaxSize, "max-size", 0, "Maximum object size in bytes (inclusive, 0 = unlimited)")
	cmd.Flags().StringVar(&findOpt.NewerThan, "newer-than", "", "Only objects modified after this time (RFC3339 or 'YYYY-MM-DD')")
	cmd.Flags().StringVar(&findOpt.OlderThan, "older-than", "", "Only objects modified before this time (RFC3339 or 'YYYY-MM-DD')")
	cmd.Flags().IntVar(&findOpt.Limit, "limit", 0, "Stop after N matching objects (0 = unlimited)")

	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func NewTreeCmd() *cobra.Command {
	var treeOpt action.TreeOptions
	opts := newCmdContext()
	cmd := &cobra.Command{
		Use:               "tree [alias:bucket[/prefix]] ...",
		Short:             "Display objects as a tree of directories",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: AutoCompletePath,
		RunE: NewRunE(func(S3 action.S3Client, _ *Context, s3path *s3path.Path) error {
			return S3.TreeObjects(treeOpt, s3path.Bucket, s3path.Key)
		}, &opts),
	}
	cmd.Flags().IntVarP(&treeOpt.MaxDepth, "max-depth", "L", 0, "Limit display depth (0 = unlimited)")
	cmd.Flags().BoolVarP(&treeOpt.ShowSize, "size", "s", false, "Show object size next to file names")
	return cmd
}
