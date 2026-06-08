package cmd

import (
	"fmt"

	"s3cli/pkg/action"
	"s3cli/pkg/utils"

	"github.com/spf13/cobra"
)

func init() {
	Register("object", "Object Operations", NewCpCmd)
	Register("object", "Object Operations", NewMvCmd)
	Register("object", "Object Operations", NewMirrorCmd)
}

// samePath 判断两个 S3 路径是否指向同一对象。
func samePath(a, b *utils.S3Path) bool {
	return a.Alias == b.Alias && a.Bucket == b.Bucket && a.Key == b.Key
}

func NewCpCmd() *cobra.Command {
	opts := newCmdContext()
	cmd := &cobra.Command{
		Use:   "cp [src-alias:bucket/key] [dst-alias:bucket/key]",
		Short: "Copy object(s) within the same S3 endpoint",
		Args:  cobra.ExactArgs(2),
		RunE: NewRunETwoPaths(func(src, dst action.S3Client, srcPath, dstPath *utils.S3Path, opts *CmdContext) error {
			if srcPath.Alias != dstPath.Alias {
				return fmt.Errorf("cp only supports same-alias copy; use `mirror` for cross-endpoint")
			}
			if samePath(srcPath, dstPath) {
				return fmt.Errorf("source and destination are the same: %s", action.S3PathStatic(srcPath.Alias, srcPath.Bucket, srcPath.Key))
			}
			return src.CopyObjects(srcPath.Bucket, srcPath.Key, dstPath.Bucket, dstPath.Key, opts.Global.Recursive, ScrollMax)
		}, &opts),
	}
	cmd.Flags().BoolVarP(&opts.Global.Recursive, "recursive", "r", false, "Copy recursively")
	return cmd
}

func NewMvCmd() *cobra.Command {
	opts := newCmdContext()
	cmd := &cobra.Command{
		Use:   "mv [src-alias:bucket/key] [dst-alias:bucket/key]",
		Short: "Move object(s) within the same S3 endpoint",
		Args:  cobra.ExactArgs(2),
		RunE: NewRunETwoPaths(func(src, dst action.S3Client, srcPath, dstPath *utils.S3Path, opts *CmdContext) error {
			if srcPath.Alias != dstPath.Alias {
				return fmt.Errorf("mv only supports same-alias move; use `mirror --remove` for cross-endpoint")
			}
			if samePath(srcPath, dstPath) {
				return fmt.Errorf("source and destination are the same: %s", action.S3PathStatic(srcPath.Alias, srcPath.Bucket, srcPath.Key))
			}
			return src.Mv(srcPath.Bucket, srcPath.Key, dstPath.Bucket, dstPath.Key, opts.Global.Recursive, ScrollMax)
		}, &opts),
	}
	cmd.Flags().BoolVarP(&opts.Global.Recursive, "recursive", "r", false, "Move recursively")
	return cmd
}

func NewMirrorCmd() *cobra.Command {
	var (
		remove      bool
		overwrite   bool
		dryRun      bool
		concurrency int
		partSizeMB  int
		sizeLimit   int64
	)

	opts := newCmdContext()
	cmd := &cobra.Command{
		Use:     "mirror [src-alias:bucket/prefix] [dst-alias:bucket/prefix]",
		Aliases: []string{"sync"},
		Short:   "Synchronize objects from source to target (one-way sync)",
		Args:    cobra.ExactArgs(2),
		RunE: NewRunETwoPaths(func(src, tgt action.S3Client, srcPath, tgtPath *utils.S3Path, opts *CmdContext) error {
			if srcPath.Bucket == "" || tgtPath.Bucket == "" {
				return fmt.Errorf("both src and dst must include a bucket")
			}
			if srcPath.Alias == tgtPath.Alias {
				return fmt.Errorf("mirror requires different aliases (use `cp` for same-alias copy)")
			}

			return action.Mirror(action.MirrorOptions{
				Src: &action.S3PathOptions{
					Client:        &src,
					Bucket:        srcPath.Bucket,
					ObjectKey:     srcPath.Key,
					TrailingSlash: srcPath.TrailingSlash,
				},
				Tgt: &action.S3PathOptions{
					Client:        &tgt,
					Bucket:        tgtPath.Bucket,
					ObjectKey:     tgtPath.Key,
					TrailingSlash: tgtPath.TrailingSlash,
				},
				Remove:      remove,
				Overwrite:   overwrite,
				DryRun:      dryRun,
				Concurrency: concurrency,
				PartSizeMB:  partSizeMB,
				SizeLimit:   sizeLimit,
				ScrollMax:   ScrollMax,
			})
		}, &opts),
	}

	f := cmd.Flags()
	f.BoolVar(&remove, "remove", false, "Delete extra objects on target that don't exist on source")
	f.BoolVar(&overwrite, "overwrite", false, "Overwrite target objects whose ETag/size/mtime differ from source")
	f.BoolVar(&dryRun, "dry-run", false, "Show what would be done without making any changes")
	f.IntVar(&concurrency, "concurrency", action.DefaultMirrorConcurrency, "Number of concurrent transfers")
	f.IntVar(&partSizeMB, "part-size", 64, "Multipart part size in MB (cross-endpoint only)")
	f.Int64Var(&sizeLimit, "size-limit", 0, "Skip objects larger than N bytes (0 = no limit)")
	return cmd
}
