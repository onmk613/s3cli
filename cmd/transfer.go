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
	Register("object", "Object Operations", NewCatCmd)
	Register("object", "Object Operations", NewPipeCmd)
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
	return cmd
}

// NewCatCmd 输出对象内容 (mc cat 对齐: --offset/-o, --tail/-t, --version-id/--vid;
// 同时兼容原 head 的 --lines/-n).
func NewCatCmd() *cobra.Command {
	var catOpt action.CatOptions
	cmd := &cobra.Command{
		Use:               "cat [alias:bucket/key] ...",
		Long:              "Print object contents to stdout (text output only)",
		Short:             "Print object contents to stdout",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: AutoCompletePath,
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.CatObject(catOpt, dst.Bucket, dst.Key)
		}),
	}
	f := cmd.Flags()
	f.StringVar(&catOpt.Range, "range", "", "HTTP Range header, e.g. 'bytes=0-1023'")
	f.Int64VarP(&catOpt.Offset, "offset", "o", 0, "Start offset in bytes")
	f.Int64VarP(&catOpt.Tail, "tail", "t", 0, "Print only the last N bytes")
	f.IntVarP(&catOpt.Lines, "lines", "n", 0, "Print only the first N lines (0 = full object)")
	f.StringVar(&catOpt.VersionID, "version-id", "", "Display a specific version of the object")
	f.StringVar(&catOpt.VersionID, "vid", "", "Alias of --version-id")
	return cmd
}

// NewPipeCmd 从 stdin 上传 (mc pipe 对齐: --tags, --storage-class/--sc).
func NewPipeCmd() *cobra.Command {
	var pipeOpt action.PipeOptions
	// cmd 先声明后赋值: RunE 闭包需要引用 cmd 判断 --part-size 是否被显式设置
	var cmd *cobra.Command
	cmd = &cobra.Command{
		Use:               "pipe [alias:bucket/key]",
		Long:              "Upload data from stdin to an S3 object (text output only)",
		Short:             "Upload data from stdin to an S3 object",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: AutoCompletePath,
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			p := pipeOpt
			if cfg, ok := config.G.S[S3.Alias]; ok {
				if cfg.DefaultMimeType != "" {
					p.DefaultMimeType = cfg.DefaultMimeType
				}
				if cfg.MultipartChunkSizeMb > 0 && !cmd.Flags().Changed("part-size") {
					p.PartSizeMB = cfg.MultipartChunkSizeMb
				}
			}
			return S3.PipeUpload(p, dst.Bucket, dst.Key)
		}),
	}
	f := cmd.Flags()
	f.StringVar(&pipeOpt.ContentType, "content-type", config.DefaultMimeType, "Content-Type of the uploaded object")
	f.IntVar(&pipeOpt.Concurrency, "concurrency", config.DefaultConcurrency, "Reserved for future parallel stream uploads")
	f.IntVar(&pipeOpt.PartSizeMB, "part-size", config.DefaultPartSizeMB, "Multipart upload part size (MB) (default: alias multipart_chunk_size_mb or 15)")
	f.StringVar(&pipeOpt.StorageClass, "storage-class", "", "Storage class")
	f.StringVar(&pipeOpt.StorageClass, "sc", "", "Alias of --storage-class")
	f.StringToStringVar(&pipeOpt.Metadata, "metadata", nil, "Custom metadata (x-amz-meta-*). Can repeat. Format: key=value")
	f.StringVar(&pipeOpt.Tags, "tags", "", "Apply tags to the uploaded object: '<key1>=<value1>&<key2>=<value2>'")
	return cmd
}
