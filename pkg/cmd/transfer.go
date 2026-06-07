package cmd

import (
	"s3cli/pkg/action"
	"s3cli/pkg/config"
	"s3cli/pkg/utils"

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
	opts := newCmdContext(ParseLocalFileAndS3Path)
	cmd := &cobra.Command{
		Use:   "get [alias:bucket/path] [local-path]",
		Short: "Download object(s) from S3",
		Args:  cobra.MatchAll(cobra.MinimumNArgs(1), cobra.MaximumNArgs(2)),
		RunE: NewRunE(ActionFunc(func(S3 action.S3Client, opts *CmdContext, s3path *utils.S3Path) error {
			g := getOpt
			g.ScrollMax = ScrollMax
			return S3.GetObject(g, s3path.Bucket, s3path.Key, opts.Global.LocalFile)
		}), &opts),
	}
	cmd.Flags().BoolVarP(&opts.Global.Recursive, "recursive", "r", false, "Operate on directories recursively")
	cmd.Flags().IntVar(&getOpt.Concurrency, "concurrency", config.DefaultConcurrency, "Number of concurrent parts to download")
	cmd.Flags().IntVar(&getOpt.PartSizeMB, "part-size", config.DefaultPartSizeMB, "Multipart download part size (MB)")
	cmd.Flags().StringVar(&getOpt.Range, "range", "", "HTTP Range header, e.g. 'bytes=0-1023' (single file only)")
	return cmd
}

func NewPutCmd() *cobra.Command {
	var putOpt action.PutOptions
	opts := newCmdContext(ParseLocalFileAndS3Path)
	cmd := &cobra.Command{
		Use:   "put [local-path] [s3://bucket/path]",
		Short: "Upload file(s) to S3",
		Args:  cobra.ExactArgs(2),
		RunE: NewRunE(ActionFunc(func(S3 action.S3Client, opts *CmdContext, s3path *utils.S3Path) error {
			p := putOpt
			p.ScrollMax = ScrollMax
			if cfg, ok := config.G.S[S3.Alias]; ok && cfg.DefaultMimeType != "" {
				p.DefaultMimeType = cfg.DefaultMimeType
			}
			return S3.PutObject(p, s3path.Bucket, s3path.Key, opts.Global.LocalFile, s3path.TrailingSlash)
		}), &opts),
	}
	cmd.Flags().BoolVarP(&opts.Global.Recursive, "recursive", "r", false, "Upload directories recursively")
	cmd.Flags().StringVar(&putOpt.ContentType, "content-type", "", "Override Content-Type (single file only)")
	cmd.Flags().IntVar(&putOpt.Concurrency, "concurrency", config.DefaultConcurrency, "Number of concurrent parts to upload")
	cmd.Flags().IntVar(&putOpt.PartSizeMB, "part-size", config.DefaultPartSizeMB, "Multipart upload part size (MB)")
	cmd.Flags().StringVar(&putOpt.StorageClass, "storage-class", "", "Storage class: STANDARD / STANDARD_IA / GLACIER / DEEP_ARCHIVE / ...")
	cmd.Flags().StringToStringVar(&putOpt.Metadata, "metadata", nil, "Custom metadata, can repeat. Format: key=value (becomes x-amz-meta-key)")
	return cmd
}

func NewRmCmd() *cobra.Command {
	opts := newCmdContext()
	cmd := &cobra.Command{
		Use:     "rm [s3://bucket/path] ...",
		Aliases: []string{"delete", "del"},
		Short:   "Delete object(s) from S3",
		Args:    cobra.MinimumNArgs(1),
		RunE: NewRunE(ActionFunc(func(S3 action.S3Client, opts *CmdContext, s3path *utils.S3Path) error {
			return S3.DeleteObjects(s3path.Bucket, s3path.Key, opts.Global.Recursive)
		}), &opts),
	}
	cmd.Flags().BoolVarP(&opts.Global.Recursive, "recursive", "r", false, "Delete recursively")
	return cmd
}

func NewCatCmd() *cobra.Command {
	var catOpt action.CatOptions
	opts := newCmdContext()
	cmd := &cobra.Command{
		Use:   "cat [alias:bucket/key] ...",
		Short: "Print object contents to stdout",
		Args:  cobra.MinimumNArgs(1),
		RunE: NewRunE(ActionFunc(func(S3 action.S3Client, _ *CmdContext, s3path *utils.S3Path) error {
			return S3.CatObject(catOpt, s3path.Bucket, s3path.Key)
		}), &opts),
	}
	cmd.Flags().StringVar(&catOpt.Range, "range", "", "HTTP Range header, e.g. 'bytes=0-1023'")
	return cmd
}

func NewPipeCmd() *cobra.Command {
	var pipeOpt action.PipeOptions
	opts := newCmdContext()
	cmd := &cobra.Command{
		Use:   "pipe [alias:bucket/key]",
		Short: "Upload data from stdin to an S3 object",
		Args:  cobra.ExactArgs(1),
		RunE: NewRunE(ActionFunc(func(S3 action.S3Client, _ *CmdContext, s3path *utils.S3Path) error {
			p := pipeOpt
			if cfg, ok := config.G.S[S3.Alias]; ok && cfg.DefaultMimeType != "" {
				p.DefaultMimeType = cfg.DefaultMimeType
			}
			return S3.PipeUpload(p, s3path.Bucket, s3path.Key)
		}), &opts),
	}
	cmd.Flags().StringVar(&pipeOpt.ContentType, "content-type", "", "Content-Type of the uploaded object")
	cmd.Flags().IntVar(&pipeOpt.Concurrency, "concurrency", config.DefaultConcurrency, "Number of concurrent parts to upload")
	cmd.Flags().IntVar(&pipeOpt.PartSizeMB, "part-size", config.DefaultPartSizeMB, "Multipart upload part size (MB)")
	cmd.Flags().StringVar(&pipeOpt.StorageClass, "storage-class", "", "Storage class")
	cmd.Flags().StringToStringVar(&pipeOpt.Metadata, "metadata", nil, "Custom metadata (x-amz-meta-*). Can repeat. Format: key=value")
	return cmd
}
