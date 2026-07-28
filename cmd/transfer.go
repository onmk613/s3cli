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

func NewGetCmd() *cobra.Command {
	var getOpt action.GetOptions
	opts := newCmdContext(ParseS3PathAndArgs)
	cmd := &cobra.Command{
		Use:               "get [alias:bucket/path] [local-path]",
		Short:             "Download object(s) from S3",
		Args:              cobra.MatchAll(cobra.MinimumNArgs(1), cobra.MaximumNArgs(2)),
		ValidArgsFunction: CompleteLocalLast(AutoCompletePath, 1),
		RunE: NewRunE(func(S3 action.S3Client, opts *Context, s3path *s3path.Path) error {
			g := getOpt
			g.Recursive = opts.Global.Recursive
			g.NoProgress = config.G.F.Quiet
			return S3.GetObject(g, s3path.Bucket, s3path.Key, opts.Global.Args)
		}, &opts),
	}
	cmd.Flags().BoolVarP(&opts.Global.Recursive, "recursive", "r", false, "Operate on directories recursively")
	cmd.Flags().IntVar(&getOpt.Concurrency, "concurrency", config.DefaultConcurrency, "Number of concurrent files to download recursively")
	cmd.Flags().IntVar(&getOpt.PartSizeMB, "part-size", config.DefaultPartSizeMB, "Reserved for future parallel single-file downloads")
	cmd.Flags().StringVar(&getOpt.Range, "range", "", "HTTP Range header, e.g. 'bytes=0-1023' (single file only)")
	cmd.Flags().BoolVar(&getOpt.Overwrite, "overwrite", false, "Overwrite existing local files (default: skip if local file exists)")
	return cmd
}

func NewPutCmd() *cobra.Command {
	var putOpt action.PutOptions
	opts := newCmdContext(ParseArgsAndS3Path)
	// cmd 先声明后赋值: RunE 闭包需要引用 cmd 判断 --part-size 是否被显式设置
	var cmd *cobra.Command
	cmd = &cobra.Command{
		Use:               "put [local-path] [alias:bucket/path]",
		Short:             "Upload file(s) to S3",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: CompleteLocalFirst(AutoCompletePath),
		RunE: NewRunE(func(S3 action.S3Client, opts *Context, s3path *s3path.Path) error {
			p := putOpt
			p.Recursive = opts.Global.Recursive
			p.NoProgress = config.G.F.Quiet
			if cfg, ok := config.G.S[S3.Alias]; ok {
				if cfg.DefaultMimeType != "" {
					p.DefaultMimeType = cfg.DefaultMimeType
				}
				// alias 配置的 chunk size 仅在 --part-size 未显式设置时生效
				if cfg.MultipartChunkSizeMb > 0 && !cmd.Flags().Changed("part-size") {
					p.PartSizeMB = cfg.MultipartChunkSizeMb
				}
			}
			return S3.PutObject(p, s3path.Bucket, s3path.Key, opts.Global.Args, s3path.TrailingSlash)
		}, &opts),
	}
	cmd.Flags().BoolVarP(&opts.Global.Recursive, "recursive", "r", false, "Upload directories recursively")
	cmd.Flags().StringVar(&putOpt.ContentType, "content-type", "", "Override Content-Type (single file only)")
	cmd.Flags().IntVar(&putOpt.Concurrency, "concurrency", config.DefaultConcurrency, "Number of concurrent files to upload recursively")
	cmd.Flags().IntVar(&putOpt.PartSizeMB, "part-size", config.DefaultPartSizeMB, "Multipart upload part size in MB for files >= 64 MiB (default: alias multipart_chunk_size_mb or 15)")
	cmd.Flags().StringVar(&putOpt.StorageClass, "storage-class", "", "Storage class: STANDARD / STANDARD_IA / GLACIER / DEEP_ARCHIVE / ...")
	cmd.Flags().StringToStringVar(&putOpt.Metadata, "metadata", nil, "Custom metadata, can repeat. Format: key=value (becomes x-amz-meta-key)")
	cmd.Flags().BoolVar(&putOpt.Overwrite, "overwrite", false, "Overwrite existing objects (default: skip if target object exists)")
	return cmd
}

func NewRmCmd() *cobra.Command {
	var delOpt action.DelOptions
	opts := newCmdContext()
	cmd := &cobra.Command{
		Use:               "rm [s3://bucket/path] ...",
		Aliases:           []string{"delete", "del"},
		Short:             "Delete object(s) from S3",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: AutoCompletePath,
		RunE: NewRunE(func(S3 action.S3Client, opts *Context, s3path *s3path.Path) error {
			return S3.DeleteObjects(s3path.Bucket, s3path.Key, delOpt)
		}, &opts),
	}
	cmd.Flags().BoolVarP(&delOpt.Recursive, "recursive", "r", false, "Delete recursively")
	cmd.Flags().StringVarP(&delOpt.VersionID, "version-id", "v", "", "Delete a specific version of the object")
	return cmd
}

func NewCatCmd() *cobra.Command {
	var catOpt action.CatOptions
	opts := newCmdContext()
	cmd := &cobra.Command{
		Use:               "cat [alias:bucket/key] ...",
		Short:             "Print object contents to stdout",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: AutoCompletePath,
		RunE: NewRunE(func(S3 action.S3Client, _ *Context, s3path *s3path.Path) error {
			return S3.CatObject(catOpt, s3path.Bucket, s3path.Key)
		}, &opts),
	}
	cmd.Flags().StringVar(&catOpt.Range, "range", "", "HTTP Range header, e.g. 'bytes=0-1023'")
	return cmd
}

func NewPipeCmd() *cobra.Command {
	var pipeOpt action.PipeOptions
	opts := newCmdContext()
	// cmd 先声明后赋值: RunE 闭包需要引用 cmd 判断 --part-size 是否被显式设置
	var cmd *cobra.Command
	cmd = &cobra.Command{
		Use:               "pipe [alias:bucket/key]",
		Short:             "Upload data from stdin to an S3 object",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: AutoCompletePath,
		RunE: NewRunE(func(S3 action.S3Client, _ *Context, s3path *s3path.Path) error {
			p := pipeOpt
			if cfg, ok := config.G.S[S3.Alias]; ok {
				if cfg.DefaultMimeType != "" {
					p.DefaultMimeType = cfg.DefaultMimeType
				}
				if cfg.MultipartChunkSizeMb > 0 && !cmd.Flags().Changed("part-size") {
					p.PartSizeMB = cfg.MultipartChunkSizeMb
				}
			}
			return S3.PipeUpload(p, s3path.Bucket, s3path.Key)
		}, &opts),
	}
	cmd.Flags().StringVar(&pipeOpt.ContentType, "content-type", config.DefaultMimeType, "Content-Type of the uploaded object")
	cmd.Flags().IntVar(&pipeOpt.Concurrency, "concurrency", config.DefaultConcurrency, "Reserved for future parallel stream uploads")
	cmd.Flags().IntVar(&pipeOpt.PartSizeMB, "part-size", config.DefaultPartSizeMB, "Multipart upload part size (MB) (default: alias multipart_chunk_size_mb or 15)")
	cmd.Flags().StringVar(&pipeOpt.StorageClass, "storage-class", "", "Storage class")
	cmd.Flags().StringToStringVar(&pipeOpt.Metadata, "metadata", nil, "Custom metadata (x-amz-meta-*). Can repeat. Format: key=value")
	return cmd
}
