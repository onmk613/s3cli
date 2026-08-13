package cmd

import (
	"s3cli/internal/action"
	"s3cli/internal/config"
	"s3cli/internal/s3path"
	"s3cli/pkg/i18n"

	"github.com/spf13/cobra"
)

func init() {
	Register("object", "Object Operations", NewGetCmd)
	Register("object", "Object Operations", NewPutCmd)
	Register("object", "Object Operations", NewRmCmd)
	Register("object", "Object Operations", NewRestoreCmd)
}

// NewGetCmd 下载对象
func NewGetCmd() *cobra.Command {
	var getOpt action.GetOptions
	cmd := &cobra.Command{
		Use:               "get [alias:bucket/path] [local-path]",
		Long:              i18n.T("Download object(s) from S3 (text output only)", "从 S3 下载对象（仅文本输出）"),
		Short:             i18n.T("Download object(s) from S3", "从 S3 下载对象"),
		Args:              cobra.MatchAll(cobra.MinimumNArgs(1), cobra.MaximumNArgs(2)),
		ValidArgsFunction: CompleteLocalLast(AutoCompletePath, 1),
		Annotations:       LastLocalFileOrPathMode,
		RunE: NewRunEWithMode(func(S3 action.Action, dst *s3path.Path, opts ArgParseMode) error {
			return S3.GetObject(getOpt, dst.Bucket, dst.Key, opts[LocalFileOrPath])
		}),
	}
	f := cmd.Flags()
	f.BoolVarP(&getOpt.Recursive, "recursive", "r", false, i18n.T("Operate on directories recursively", "递归处理目录"))
	f.IntVar(&getOpt.Concurrency, "concurrency", config.DefaultConcurrency, i18n.T("Number of concurrent files to download recursively", "递归下载时的并发文件数"))
	f.StringVar(&getOpt.Range, "range", "", i18n.T("HTTP Range header, e.g. 'bytes=0-1023' (single file only)", "HTTP Range 请求头，如 'bytes=0-1023'（仅限单文件）"))
	f.BoolVar(&getOpt.Overwrite, "overwrite", false, i18n.T("Overwrite existing local files (default: skip if local file exists)", "覆盖已存在的本地文件（默认：本地文件存在时跳过）"))
	f.StringVarP(&getOpt.VersionID, "version-id", "v", "", i18n.T("Download a specific version of the object", "下载对象的特定版本"))
	// stdout 模式 (get <alias:bucket/key> -): 替代旧 cat 命令
	f.Int64VarP(&getOpt.Offset, "offset", "o", 0, i18n.T("Start byte offset when streaming to stdout (use with 'get -')", "流式输出到 stdout 时的起始字节偏移（配合 'get -' 使用）"))
	f.Int64VarP(&getOpt.Tail, "tail", "t", 0, i18n.T("Output only the last N bytes (use with 'get -')", "只输出最后 N 字节（配合 'get -' 使用）"))
	f.IntVarP(&getOpt.Lines, "lines", "n", 0, i18n.T("Output only the first N lines (use with 'get -')", "只输出前 N 行（配合 'get -' 使用）"))
	f.BoolVarP(&getOpt.NoProgress, "quiet", "q", false, quietDesc())
	return cmd
}

// NewPutCmd 上传对象 (--tags, --storage-class/--sc).
func NewPutCmd() *cobra.Command {
	var putOpt action.PutOptions
	// cmd 先声明后赋值: RunE 闭包需要引用 cmd 判断 --part-size 是否被显式设置
	var cmd *cobra.Command
	cmd = &cobra.Command{
		Use:               "put [local-path] [alias:bucket/path]",
		Long:              i18n.T("Upload file(s) to S3 (text output only)", "上传文件到 S3（仅文本输出）"),
		Short:             i18n.T("Upload file(s) to S3", "上传文件到 S3"),
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: CompleteLocalFirst(AutoCompletePath),
		Annotations:       FirstLocalFileOrPathMode,
		RunE: NewRunEWithMode(func(S3 action.Action, dst *s3path.Path, opts ArgParseMode) error {
			if cfg, ok := config.G.S[S3.Alias]; ok {
				if cfg.DefaultMimeType != "" {
					putOpt.DefaultMimeType = cfg.DefaultMimeType
				}
				// alias 配置的 chunk size 仅在 --part-size 未显式设置时生效
				if cfg.MultipartChunkSizeMb > 0 && !cmd.Flags().Changed("part-size") {
					putOpt.PartSizeMB = cfg.MultipartChunkSizeMb
				}
			}
			return S3.PutObject(putOpt, dst.Bucket, dst.Key, opts[LocalFileOrPath], dst.TrailingSlash)
		}),
	}
	f := cmd.Flags()
	f.BoolVarP(&putOpt.Recursive, "recursive", "r", false, i18n.T("Upload directories recursively", "递归上传目录"))
	f.StringVar(&putOpt.ContentType, "content-type", "", i18n.T("Override Content-Type (single file only)", "覆盖 Content-Type（仅限单文件）"))
	f.IntVar(&putOpt.Concurrency, "concurrency", config.DefaultConcurrency, i18n.T("Number of concurrent files to upload recursively", "递归上传时的并发文件数"))
	f.IntVar(&putOpt.PartSizeMB, "part-size", config.DefaultPartSizeMB, i18n.T("Multipart upload part size in MB for files >= 64 MiB (default: alias multipart_chunk_size_mb or 15)", "文件 >= 64 MiB 时分段上传的分段大小（MB）（默认：alias 的 multipart_chunk_size_mb 或 15）"))
	f.StringVar(&putOpt.StorageClass, "storage-class", "", i18n.T("Storage class: STANDARD / STANDARD_IA / GLACIER / DEEP_ARCHIVE / ...", "存储类型：STANDARD / STANDARD_IA / GLACIER / DEEP_ARCHIVE / ..."))
	f.StringVar(&putOpt.StorageClass, "sc", "", scAliasDesc())
	f.StringToStringVar(&putOpt.Metadata, "metadata", nil, i18n.T("Custom metadata, can repeat. Format: key=value (becomes x-amz-meta-key)", "自定义元数据，可重复。格式：key=value（将成为 x-amz-meta-key）"))
	f.StringVar(&putOpt.Tags, "tags", "", i18n.T("Apply tags to the uploaded object: '<key1>=<value1>&<key2>=<value2>'", "给上传对象添加标签：'<key1>=<value1>&<key2>=<value2>'"))
	f.BoolVar(&putOpt.Overwrite, "overwrite", false, i18n.T("Overwrite existing objects (default: skip if target object exists)", "覆盖已存在的对象（默认：目标对象存在时跳过）"))
	f.BoolVarP(&putOpt.NoProgress, "quiet", "q", false, quietDesc())
	return cmd
}

// NewRmCmd 删除对象 (--force, --versions, --incomplete/-I,
// --dry-run, --older-than/--newer-than, --stdin, --non-current, --version-id/--vid).
func NewRmCmd() *cobra.Command {
	var delOpt action.DelOptions
	cmd := &cobra.Command{
		Use:               "rm [alias:bucket/path] ...",
		Long:              i18n.T("Delete object(s) from S3 (text output only)", "删除 S3 对象（仅文本输出）"),
		Aliases:           []string{"delete", "del"},
		Short:             i18n.T("Delete object(s) from S3", "删除 S3 对象"),
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: AutoCompletePath,
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			if delOpt.Stdin {
				return S3.DeleteObjects(dst.Bucket, "", delOpt)
			}
			return S3.DeleteObjects(dst.Bucket, dst.Key, delOpt)
		}),
	}
	f := cmd.Flags()
	f.BoolVarP(&delOpt.Recursive, "recursive", "r", false, i18n.T("Delete recursively", "递归删除"))
	f.BoolVar(&delOpt.Force, "force", false, i18n.T("Allow a recursive remove operation (required with -r)", "允许递归删除操作（配合 -r 使用）"))
	f.StringVar(&delOpt.VersionID, "version-id", "", i18n.T("Delete a specific version of the object", "删除对象的特定版本"))
	f.StringVar(&delOpt.VersionID, "vid", "", vidAliasDesc())
	f.BoolVar(&delOpt.Versions, "versions", false, i18n.T("Delete the object(s) and all its versions", "删除对象及其所有版本"))
	f.BoolVarP(&delOpt.Incomplete, "incomplete", "I", false, i18n.T("Abort in-progress multipart uploads under the prefix", "中止前缀下进行中的分段上传任务"))
	f.BoolVar(&delOpt.DryRun, "dry-run", false, i18n.T("Perform a fake remove operation (print what would be deleted)", "模拟删除操作（只打印将被删除的内容）"))
	f.StringVar(&delOpt.OlderThan, "older-than", "", i18n.T("Delete objects older than a duration (7d10h31s) or absolute time", "删除早于某时长（7d10h31s）或绝对时间的对象"))
	f.StringVar(&delOpt.NewerThan, "newer-than", "", i18n.T("Delete objects newer than a duration (7d10h31s) or absolute time", "删除晚于某时长（7d10h31s）或绝对时间的对象"))
	f.BoolVar(&delOpt.Stdin, "stdin", false, i18n.T("Read object names from STDIN", "从标准输入读取对象名"))
	f.BoolVar(&delOpt.NonCurrent, "non-current", false, i18n.T("Delete object versions that are non-current", "删除非当前版本的对象"))
	f.StringSliceVar(&delOpt.Include, "include", nil, i18n.T("Only delete keys matching this glob (can repeat; use with -r)", "只删除匹配该通配模式的 key（可重复；配合 -r 使用）"))
	f.StringSliceVar(&delOpt.Exclude, "exclude", nil, i18n.T("Skip keys matching this glob (can repeat)", "跳过匹配该通配模式的 key（可重复）"))
	return cmd
}

// NewRestoreCmd 请求恢复归档对象 (Glacier / DEEP_ARCHIVE 等) 的可访问副本.
func NewRestoreCmd() *cobra.Command {
	var opt action.RestoreOptions
	cmd := &cobra.Command{
		Use:               "restore [alias:bucket/key] ...",
		Long:              i18n.T("Request restoration of archived objects (text output only)", "请求恢复归档对象的可访问副本（仅文本输出）"),
		Short:             i18n.T("Restore archived objects (Glacier / Deep Archive)", "恢复归档对象（Glacier / Deep Archive）"),
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: AutoCompletePath,
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.Restore(opt, dst.Bucket, dst.Key)
		}),
	}
	f := cmd.Flags()
	f.IntVar(&opt.Days, "days", 1, i18n.T("Number of days to keep the restored copy accessible", "恢复副本保持可访问的天数"))
	f.StringVar(&opt.Tier, "tier", "", i18n.T("Restore tier: Expedited / Standard / Bulk (default: Standard)", "恢复层级：Expedited / Standard / Bulk（默认：Standard）"))
	f.StringVar(&opt.VersionID, "version-id", "", i18n.T("Restore a specific object version", "恢复对象的特定版本"))
	f.StringVar(&opt.VersionID, "vid", "", vidAliasDesc())
	return cmd
}
