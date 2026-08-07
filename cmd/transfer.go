package cmd

import (
	"s3cli/internal/action"
	"s3cli/internal/config"
	"s3cli/internal/s3path"

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
		Long:              "Download object(s) from S3 (text output only)",
		Short:             "Download object(s) from S3",
		Args:              cobra.MatchAll(cobra.MinimumNArgs(1), cobra.MaximumNArgs(2)),
		ValidArgsFunction: CompleteLocalLast(AutoCompletePath, 1),
		Annotations:       ParseS3PathAndArgs,
		RunE: NewRunEWithMode(func(S3 action.Action, dst *s3path.Path, opts ArgParseMode) error {
			return S3.GetObject(getOpt, dst.Bucket, dst.Key, opts[AddedArgs])
		}),
	}
	f := cmd.Flags()
	f.BoolVarP(&getOpt.Recursive, "recursive", "r", false, "Operate on directories recursively")
	f.IntVar(&getOpt.Concurrency, "concurrency", config.DefaultConcurrency, "Number of concurrent files to download recursively")
	f.StringVar(&getOpt.Range, "range", "", "HTTP Range header, e.g. 'bytes=0-1023' (single file only)")
	f.BoolVar(&getOpt.Overwrite, "overwrite", false, "Overwrite existing local files (default: skip if local file exists)")
	f.StringVarP(&getOpt.VersionID, "version-id", "v", "", "Download a specific version of the object")
	// stdout 模式 (get <alias:bucket/key> -): 替代旧 cat 命令
	f.Int64VarP(&getOpt.Offset, "offset", "o", 0, "Start byte offset when streaming to stdout (use with 'get -')")
	f.Int64VarP(&getOpt.Tail, "tail", "t", 0, "Output only the last N bytes (use with 'get -')")
	f.IntVarP(&getOpt.Lines, "lines", "n", 0, "Output only the first N lines (use with 'get -')")
	f.BoolVarP(&getOpt.NoProgress, "quiet", "q", false, "Disable progress bar; stream plain text output instead")
	return cmd
}

// NewPutCmd 上传对象 (mc put 对齐: --tags, --storage-class/--sc).
func NewPutCmd() *cobra.Command {
	var putOpt action.PutOptions
	// cmd 先声明后赋值: RunE 闭包需要引用 cmd 判断 --part-size 是否被显式设置
	var cmd *cobra.Command
	cmd = &cobra.Command{
		Use:               "put [local-path] [alias:bucket/path]",
		Long:              "Upload file(s) to S3 (text output only)",
		Short:             "Upload file(s) to S3",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: CompleteLocalFirst(AutoCompletePath),
		Annotations:       ParseArgsAndS3Path,
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
			return S3.PutObject(putOpt, dst.Bucket, dst.Key, opts[AddedArgs], dst.TrailingSlash)
		}),
	}
	f := cmd.Flags()
	f.BoolVarP(&putOpt.Recursive, "recursive", "r", false, "Upload directories recursively")
	f.StringVar(&putOpt.ContentType, "content-type", "", "Override Content-Type (single file only)")
	f.IntVar(&putOpt.Concurrency, "concurrency", config.DefaultConcurrency, "Number of concurrent files to upload recursively")
	f.IntVar(&putOpt.PartSizeMB, "part-size", config.DefaultPartSizeMB, "Multipart upload part size in MB for files >= 64 MiB (default: alias multipart_chunk_size_mb or 15)")
	f.StringVar(&putOpt.StorageClass, "storage-class", "", "Storage class: STANDARD / STANDARD_IA / GLACIER / DEEP_ARCHIVE / ...")
	f.StringVar(&putOpt.StorageClass, "sc", "", "Alias of --storage-class")
	f.StringToStringVar(&putOpt.Metadata, "metadata", nil, "Custom metadata, can repeat. Format: key=value (becomes x-amz-meta-key)")
	f.StringVar(&putOpt.Tags, "tags", "", "Apply tags to the uploaded object: '<key1>=<value1>&<key2>=<value2>'")
	f.BoolVar(&putOpt.Overwrite, "overwrite", false, "Overwrite existing objects (default: skip if target object exists)")
	f.BoolVarP(&putOpt.NoProgress, "quiet", "q", false, "Disable progress bar; stream plain text output instead")
	return cmd
}

// NewRmCmd 删除对象 (mc rm 对齐: --force, --versions, --incomplete/-I,
// --dry-run, --older-than/--newer-than, --stdin, --non-current, --version-id/--vid).
func NewRmCmd() *cobra.Command {
	var delOpt action.DelOptions
	cmd := &cobra.Command{
		Use:               "rm [alias:bucket/path] ...",
		Long:              "Delete object(s) from S3 (text output only)",
		Aliases:           []string{"delete", "del"},
		Short:             "Delete object(s) from S3",
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
	f.BoolVarP(&delOpt.Recursive, "recursive", "r", false, "Delete recursively")
	f.BoolVar(&delOpt.Force, "force", false, "Allow a recursive remove operation (required with -r)")
	f.StringVar(&delOpt.VersionID, "version-id", "", "Delete a specific version of the object")
	f.StringVar(&delOpt.VersionID, "vid", "", "Alias of --version-id")
	f.BoolVar(&delOpt.Versions, "versions", false, "Delete the object(s) and all its versions")
	f.BoolVarP(&delOpt.Incomplete, "incomplete", "I", false, "Abort in-progress multipart uploads under the prefix")
	f.BoolVar(&delOpt.DryRun, "dry-run", false, "Perform a fake remove operation (print what would be deleted)")
	f.StringVar(&delOpt.OlderThan, "older-than", "", "Delete objects older than a duration (7d10h31s) or absolute time")
	f.StringVar(&delOpt.NewerThan, "newer-than", "", "Delete objects newer than a duration (7d10h31s) or absolute time")
	f.BoolVar(&delOpt.Stdin, "stdin", false, "Read object names from STDIN")
	f.BoolVar(&delOpt.NonCurrent, "non-current", false, "Delete object versions that are non-current")
	f.StringSliceVar(&delOpt.Include, "include", nil, "Only delete keys matching this glob (can repeat; use with -r)")
	f.StringSliceVar(&delOpt.Exclude, "exclude", nil, "Skip keys matching this glob (can repeat)")
	return cmd
}

// NewRestoreCmd 请求恢复归档对象 (Glacier / DEEP_ARCHIVE 等) 的可访问副本.
func NewRestoreCmd() *cobra.Command {
	var opt action.RestoreOptions
	cmd := &cobra.Command{
		Use:               "restore [alias:bucket/key] ...",
		Long:              "Request restoration of archived objects (text output only)",
		Short:             "Restore archived objects (Glacier / Deep Archive)",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: AutoCompletePath,
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.Restore(opt, dst.Bucket, dst.Key)
		}),
	}
	f := cmd.Flags()
	f.IntVar(&opt.Days, "days", 1, "Number of days to keep the restored copy accessible")
	f.StringVar(&opt.Tier, "tier", "", "Restore tier: Expedited / Standard / Bulk (default: Standard)")
	f.StringVar(&opt.VersionID, "version-id", "", "Restore a specific object version")
	f.StringVar(&opt.VersionID, "vid", "", "Alias of --version-id")
	return cmd
}
