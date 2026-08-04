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

// NewFindCmd 对象搜索 (mc find 对齐: --name/--regex/--path/--larger/--smaller/
// --newer-than/--older-than/--maxdepth/--ignore/--print).
func NewFindCmd() *cobra.Command {
	var findOpt action.FindOptions
	var largerStr, smallerStr string
	cmd := &cobra.Command{
		Use:               "find [alias:bucket[/prefix]] ...",
		Short:             "Search objects by name pattern, size and modification time",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: AutoCompletePath,
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			if largerStr != "" {
				n, err := action.ParseByteSize(largerStr)
				if err != nil {
					return err
				}
				findOpt.Larger = n
			}
			if smallerStr != "" {
				n, err := action.ParseByteSize(smallerStr)
				if err != nil {
					return err
				}
				findOpt.Smaller = n
			}
			return S3.FindObjects(findOpt, dst.Bucket, dst.Key)
		}),
	}
	f := cmd.Flags()
	f.StringVar(&findOpt.Name, "name", "", "Match object name (shell glob, e.g. '*.log')")
	f.StringVar(&findOpt.Regex, "regex", "", "Match object key with RE2 regex pattern")
	f.StringVar(&findOpt.Path, "path", "", "Match directory names with a wildcard pattern")
	f.StringVar(&largerStr, "larger", "", "Match objects larger than this size, e.g. 10MB / 1MiB")
	f.StringVar(&smallerStr, "smaller", "", "Match objects smaller than this size, e.g. 10MB / 1MiB")
	f.StringVar(&findOpt.NewerThan, "newer-than", "", "Match objects newer than a duration (7d10h31s) or absolute time")
	f.StringVar(&findOpt.OlderThan, "older-than", "", "Match objects older than a duration (7d10h31s) or absolute time")
	f.IntVar(&findOpt.MaxDepth, "maxdepth", 0, "Limit directory navigation to this depth (0 = unlimited)")
	f.StringSliceVar(&findOpt.Ignore, "ignore", nil, "Exclude objects matching a wildcard pattern (can repeat)")
	f.StringVar(&findOpt.Print, "print", "", "Print in custom format: {name} {size} {time} {url} {path}")
	f.IntVar(&findOpt.Limit, "limit", 0, "Stop after N matching objects (0 = unlimited)")
	f.BoolVar(&findOpt.JSON, "json", false, "Output format: text or json (supported commands emit structured results)")
	return cmd
}

// NewTreeCmd 目录树展示 (mc tree 对齐: -f/--files, -d/--depth).
func NewTreeCmd() *cobra.Command {
	var treeOpt action.TreeOptions
	cmd := &cobra.Command{
		Use:               "tree [alias:bucket[/prefix]] ...",
		Short:             "Display objects as a tree of directories",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: AutoCompletePath,
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.TreeObjects(treeOpt, dst.Bucket, dst.Key)
		}),
	}
	f := cmd.Flags()
	f.IntVarP(&treeOpt.MaxDepth, "depth", "d", 0, "Limit display depth (0 = unlimited)")
	f.BoolVar(&treeOpt.Files, "files", false, "Include files in the tree (default: directories only)")
	f.BoolVarP(&treeOpt.ShowSize, "size", "s", false, "Show object size next to file names")
	f.IntVarP(&treeOpt.MaxDepth, "max-depth", "L", 0, "DEPRECATED: use -d/--depth")
	_ = cmd.Flags().MarkDeprecated("max-depth", "use -d/--depth instead")
	f.BoolVar(&treeOpt.JSON, "json", false, "Output format: text or json (supported commands emit structured results)")
	return cmd
}
