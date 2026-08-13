package cmd

import (
	"s3cli/internal/action"
	"s3cli/internal/s3path"
	"s3cli/pkg/i18n"

	"github.com/spf13/cobra"
)

func init() {
	Register("read", "Read Commands", NewLsCmd)
	Register("read", "Read Commands", NewDuCmd)
	Register("read", "Read Commands", NewStatCmd)
	Register("read", "Read Commands", NewInfoCmd)
}

// NewLsCmd 列出桶/对象 (-r/--recursive, --versions, --incomplete/-I, --summarize).
// 允许「仅别名」参数 (列出所有桶), 见 NewRunEAllowAliasOnly.
func NewLsCmd() *cobra.Command {
	var opt action.ListOptions
	cmd := &cobra.Command{
		Use:               "ls alias:[bucket/[path]]",
		Aliases:           []string{"list", "l"},
		Short:             i18n.T("List objects or bucket", "列出对象或存储桶"),
		ValidArgsFunction: AutoCompletePath,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunEAllowAliasOnly(func(S3 action.Action, dst *s3path.Path) error {
			return S3.ListObjects(opt, dst.Bucket, dst.Key)
		}),
	}
	f := cmd.Flags()
	f.BoolVar(&opt.Versions, "versions", false, i18n.T("List all versions of objects (including delete markers)", "列出对象的所有版本（含删除标记）"))
	f.BoolVarP(&opt.Incomplete, "incomplete", "I", false, i18n.T("List in-progress multipart uploads", "列出进行中的分段上传任务"))
	f.BoolVar(&opt.Summarize, "summarize", false, i18n.T("Print summary (number of objects, total size)", "打印汇总信息（对象数量、总大小）"))
	f.BoolVarP(&opt.Recursive, "recursive", "r", false, i18n.T("Recursively list all objects under the path (no delimiter)", "递归列出路径下的所有对象（不使用分隔符）"))
	f.StringSliceVar(&opt.Include, "include", nil, i18n.T("Only list keys matching this glob (can repeat; best with -r)", "只列出匹配该通配模式的 key（可重复；建议配合 -r 使用）"))
	f.StringSliceVar(&opt.Exclude, "exclude", nil, i18n.T("Skip keys matching this glob (can repeat)", "跳过匹配该通配模式的 key（可重复）"))
	f.BoolVar(&opt.JSON, "json", false, i18n.T("Output format: text or json (supported commands emit structured results)", "输出格式：text 或 json（受支持的命令输出结构化结果）"))
	return cmd
}

// NewDuCmd 磁盘占用统计 (-r/--recursive, -d/--depth).
func NewDuCmd() *cobra.Command {
	var opt action.DuOptions
	cmd := &cobra.Command{
		Use:               "du [alias:bucket/path] ...",
		Short:             i18n.T("Show disk usage of bucket or paths", "显示存储桶或路径的磁盘占用"),
		ValidArgsFunction: AutoCompletePath,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.DuObject(opt, dst.Bucket, dst.Key)
		}),
	}
	f := cmd.Flags()
	f.BoolVarP(&opt.Recursive, "recursive", "r", false, i18n.T("Print the total for each directory prefix", "打印每个目录前缀的总计"))
	f.IntVarP(&opt.Depth, "depth", "d", 0, i18n.T("Print totals only for prefixes N or fewer levels below the argument (with -r)", "只打印参数之下 N 层以内前缀的总计（配合 -r 使用）"))
	f.BoolVar(&opt.JSON, "json", false, jsonOutputDesc())
	return cmd
}

// NewStatCmd 展示对象/桶元信息 (-r/--recursive, --version-id/--vid, --json).
func NewStatCmd() *cobra.Command {
	var opt action.StatOptions
	cmd := &cobra.Command{
		Use:               "stat [alias:bucket[/path]] ...",
		Short:             i18n.T("Show metadata about bucket or object", "显示存储桶或对象的元信息"),
		ValidArgsFunction: AutoCompletePath,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.StatObjects(opt, dst.Bucket, dst.Key)
		}),
	}
	f := cmd.Flags()
	f.BoolVarP(&opt.Recursive, "recursive", "r", false, i18n.T("Stat all objects recursively under the prefix", "递归统计前缀下的所有对象"))
	f.StringVar(&opt.VersionID, "version-id", "", i18n.T("Stat a specific object version", "查看对象的特定版本"))
	f.StringVar(&opt.VersionID, "vid", "", i18n.T("Alias of --version-id", "--version-id 的别名"))
	f.BoolVar(&opt.JSON, "json", false, jsonOutputDesc())
	return cmd
}

// NewInfoCmd 查看元信息 (JSON 输出, 兼容旧用法).
func NewInfoCmd() *cobra.Command {
	var opt action.InfoOptions
	cmd := &cobra.Command{
		Use:               "info [alias:bucket[/path]] ...",
		Short:             i18n.T("Show object/bucket metadata as JSON", "以 JSON 展示对象/存储桶元信息"),
		ValidArgsFunction: AutoCompletePath,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.Info(opt, dst.Bucket, dst.Key)
		}),
	}
	f := cmd.Flags()
	f.BoolVarP(&opt.Recursive, "recursive", "r", false, i18n.T("Show all objects recursively under the prefix", "递归展示前缀下的所有对象"))
	f.StringVar(&opt.VersionID, "version-id", "", i18n.T("Show a specific object version", "查看对象的特定版本"))
	f.StringVar(&opt.VersionID, "vid", "", vidAliasDesc())
	return cmd
}

// (已移除 NewLsVersionsCmd: 列举版本请使用 `ls --versions`.)
