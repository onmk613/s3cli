package cmd

import (
	"s3cli/internal/action"
	"s3cli/internal/s3path"
	"s3cli/pkg/i18n"

	"github.com/spf13/cobra"
)

func init() {
	Register("read", "Read Commands", NewFindCmd)
	Register("read", "Read Commands", NewTreeCmd)
}

// NewFindCmd 对象搜索 (--name/--regex/--path/--larger/--smaller/
// --newer-than/--older-than/--min-depth/--max-depth/--type/--storage-class/
// --include/--exclude/--ignore/--sort/--reverse/--print).
func NewFindCmd() *cobra.Command {
	var findOpt action.FindOptions
	var largerStr, smallerStr string
	cmd := &cobra.Command{
		Use:               "find [alias:bucket[/prefix]] ...",
		Short:             i18n.T("Search objects by name pattern, size and modification time", "按名称模式、大小和修改时间搜索对象"),
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
	f.StringVar(&findOpt.Name, "name", "", i18n.T("Match object name (shell glob, e.g. '*.log')", "匹配对象名（shell 通配符，如 '*.log'）"))
	f.StringVar(&findOpt.Regex, "regex", "", i18n.T("Match object key with RE2 regex pattern", "用 RE2 正则表达式匹配对象 key"))
	f.StringVar(&findOpt.Path, "path", "", i18n.T("Match directory names with a wildcard pattern", "用通配模式匹配目录名"))
	f.StringVar(&largerStr, "larger", "", i18n.T("Match objects larger than this size, e.g. 10MB / 1MiB", "匹配大于该大小的对象，如 10MB / 1MiB"))
	f.StringVar(&smallerStr, "smaller", "", i18n.T("Match objects smaller than this size, e.g. 10MB / 1MiB", "匹配小于该大小的对象，如 10MB / 1MiB"))
	f.StringVar(&findOpt.NewerThan, "newer-than", "", i18n.T("Match objects modified after a duration or date, e.g. 3d / 7d10h31s / 2026-08-01", "匹配修改时间晚于某时长或日期的对象，如 3d / 7d10h31s / 2026-08-01"))
	f.StringVar(&findOpt.OlderThan, "older-than", "", i18n.T("Match objects modified before a duration or date, e.g. 3d / 7d10h31s / 2026-08-01", "匹配修改时间早于某时长或日期的对象，如 3d / 7d10h31s / 2026-08-01"))
	f.IntVar(&findOpt.MinDepth, "min-depth", 0, i18n.T("Skip objects shallower than this directory depth (0 = unlimited)", "跳过目录深度小于该值的对象（0 = 不限制）"))
	// 命名与 tree 的 --max-depth 统一 (min-depth/maxdepth 拼写不一致是记忆负担);
	// 旧拼写保留为弃用别名, 不破坏已有脚本。
	f.IntVar(&findOpt.MaxDepth, "max-depth", 0, i18n.T("Limit directory navigation to this depth (0 = unlimited)", "限制目录遍历深度（0 = 不限制）"))
	f.IntVar(&findOpt.MaxDepth, "maxdepth", 0, i18n.T("DEPRECATED: use --max-depth", "已弃用：请使用 --max-depth"))
	_ = cmd.Flags().MarkDeprecated("maxdepth", i18n.T("use --max-depth instead", "请改用 --max-depth"))
	f.StringVar(&findOpt.Type, "type", "", i18n.T("Match by object type: file or dir (dir matches zero-byte directory markers)", "按对象类型匹配：file 或 dir（dir 匹配零字节目录标记）"))
	f.StringVar(&findOpt.StorageClass, "storage-class", "", i18n.T("Match by storage class, e.g. STANDARD / STANDARD_IA / GLACIER", "按存储类型匹配，如 STANDARD / STANDARD_IA / GLACIER"))
	f.StringSliceVar(&findOpt.Include, "include", nil, i18n.T("Only match keys matching a glob pattern (can repeat)", "只匹配符合通配模式的 key（可重复）"))
	f.StringSliceVar(&findOpt.Exclude, "exclude", nil, i18n.T("Skip keys matching a glob pattern (can repeat)", "跳过匹配通配模式的 key（可重复）"))
	f.StringSliceVar(&findOpt.Ignore, "ignore", nil, i18n.T("Exclude objects matching a wildcard pattern (can repeat)", "排除匹配通配模式的对象（可重复）"))
	f.StringVar(&findOpt.Sort, "sort", "", i18n.T("Sort results by name, size or time (prefix with - for descending order)", "按名称、大小或时间排序（前缀 - 表示降序）"))
	f.BoolVar(&findOpt.Reverse, "reverse", false, i18n.T("Reverse the sort order", "反转排序顺序"))
	f.StringVar(&findOpt.Print, "print", "", i18n.T("Print in custom format: {name} {size} {time} {url} {path} {etag} {storage-class} {version-id}", "按自定义格式打印：{name} {size} {time} {url} {path} {etag} {storage-class} {version-id}"))
	f.IntVar(&findOpt.Limit, "limit", 0, i18n.T("Stop after N matching objects (0 = unlimited)", "匹配 N 个对象后停止（0 = 不限制）"))
	f.BoolVar(&findOpt.Versions, "versions", false, i18n.T("Filter by the latest version time via ListObjectVersions (includes delete markers)", "通过 ListObjectVersions 按最新版本时间过滤（含删除标记）"))
	f.BoolVar(&findOpt.JSON, "json", false, jsonOutputDesc())
	return cmd
}

// NewTreeCmd 目录树展示 (-f/--files, -d/--depth).
func NewTreeCmd() *cobra.Command {
	var treeOpt action.TreeOptions
	cmd := &cobra.Command{
		Use:               "tree [alias:bucket[/prefix]] ...",
		Short:             i18n.T("Display objects as a tree of directories", "以目录树形式展示对象"),
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: AutoCompletePath,
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.TreeObjects(treeOpt, dst.Bucket, dst.Key)
		}),
	}
	f := cmd.Flags()
	f.IntVarP(&treeOpt.MaxDepth, "depth", "d", 0, i18n.T("Limit display depth (0 = unlimited)", "限制展示深度（0 = 不限制）"))
	f.BoolVar(&treeOpt.Files, "files", false, i18n.T("Include files in the tree (default: directories only)", "在树中包含文件（默认：只显示目录）"))
	f.BoolVarP(&treeOpt.ShowSize, "size", "s", false, i18n.T("Show object size next to file names", "在文件名旁显示对象大小"))
	f.IntVarP(&treeOpt.MaxDepth, "max-depth", "L", 0, i18n.T("DEPRECATED: use -d/--depth", "已弃用：请使用 -d/--depth"))
	_ = cmd.Flags().MarkDeprecated("max-depth", i18n.T("use -d/--depth instead", "请改用 -d/--depth"))
	f.BoolVar(&treeOpt.JSON, "json", false, jsonOutputDesc())
	return cmd
}
