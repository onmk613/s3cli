package cmd

import (
	"fmt"
	"s3cli/internal/s3path"

	"s3cli/internal/action"
	"s3cli/internal/config"

	"github.com/spf13/cobra"
)

func init() {
	Register("object", "Object Operations", NewCpCmd)
	Register("object", "Object Operations", NewMvCmd)
	Register("sync", "Synchronization", NewMirrorCmd)
}

// samePath 判断两个 S3 路径是否指向同一对象。
func samePath(a, b *s3path.Path) bool {
	return a.Alias == b.Alias && a.Bucket == b.Bucket && a.Key == b.Key
}

func NewCpCmd() *cobra.Command {
	opts := newCmdContext()
	cmd := &cobra.Command{
		Use:               "cp [src-alias:bucket/key] [dst-alias:bucket/key]",
		Short:             "Copy object within the same S3 endpoint",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: AutoCompletePath,
		RunE: NewRunETwoPaths(func(src, dst action.S3Client, srcPath, dstPath *s3path.Path, opts *Context) error {
			if srcPath.Alias != dstPath.Alias {
				return fmt.Errorf("cp only supports same-alias copy; use `mirror` for cross-endpoint")
			}
			if samePath(srcPath, dstPath) {
				return fmt.Errorf("source and destination are the same: %s", action.S3PathStatic(srcPath.Alias, srcPath.Bucket, srcPath.Key))
			}
			return src.CopyObjects(srcPath.Bucket, srcPath.Key, dstPath.Bucket, dstPath.Key, opts.Global.Recursive, config.G.F.Quiet)
		}, &opts),
	}
	cmd.Flags().BoolVarP(&opts.Global.Recursive, "recursive", "r", false, "Copy recursively")
	return cmd
}

func NewMvCmd() *cobra.Command {
	opts := newCmdContext()
	cmd := &cobra.Command{
		Use:               "mv [src-alias:bucket/key] [dst-alias:bucket/key]",
		Short:             "Move object within the same S3 endpoint",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: AutoCompletePath,
		RunE: NewRunETwoPaths(func(src, dst action.S3Client, srcPath, dstPath *s3path.Path, opts *Context) error {
			if srcPath.Alias != dstPath.Alias {
				return fmt.Errorf("mv only supports same-alias move; use `mirror --remove` for cross-endpoint")
			}
			if samePath(srcPath, dstPath) {
				return fmt.Errorf("source and destination are the same: %s", action.S3PathStatic(srcPath.Alias, srcPath.Bucket, srcPath.Key))
			}
			return src.Mv(srcPath.Bucket, srcPath.Key, dstPath.Bucket, dstPath.Key, opts.Global.Recursive, config.G.F.Quiet)
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
		maxDelete   int
		include     []string
		exclude     []string
		manifest    string
		resume      bool
	)

	opts := newCmdContext()
	// cmd 先声明后赋值: RunE 闭包需要引用 cmd 判断 --part-size 是否被显式设置
	var cmd *cobra.Command
	cmd = &cobra.Command{
		Use:               "mirror [src-alias:bucket/prefix] [dst-alias:bucket/prefix]",
		Aliases:           []string{"sync"},
		Short:             "Synchronize objects from source to target (one-way sync)",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: AutoCompletePath,
		RunE: NewRunETwoPaths(func(src, tgt action.S3Client, srcPath, tgtPath *s3path.Path, opts *Context) error {
			if srcPath.Bucket == "" || tgtPath.Bucket == "" {
				return fmt.Errorf("both src and dst must include a bucket")
			}
			if srcPath.Alias == tgtPath.Alias {
				return fmt.Errorf("mirror requires different aliases (use `cp` for same-alias copy)")
			}

			// alias 配置的 chunk size 仅在 --part-size 未显式设置时生效;
			// 跨端 MPU 在目标端创建, 故取目标 alias 的配置。
			if cfg, ok := config.G.S[tgtPath.Alias]; ok && cfg.MultipartChunkSizeMb > 0 && !cmd.Flags().Changed("part-size") {
				partSizeMB = cfg.MultipartChunkSizeMb
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
				Remove:       remove,
				Overwrite:    overwrite,
				DryRun:       dryRun,
				Concurrency:  concurrency,
				PartSizeMB:   partSizeMB,
				SizeLimit:    sizeLimit,
				MaxDelete:    maxDelete,
				Include:      include,
				Exclude:      exclude,
				ManifestPath: manifest,
				Resume:       resume,
				NoProgress:   config.G.F.Quiet,
			})
		}, &opts),
	}

	f := cmd.Flags()
	f.BoolVar(&remove, "remove", false, "Delete extra objects on target that don't exist on source")
	f.BoolVar(&overwrite, "overwrite", false, "Overwrite target objects whose ETag/size/mtime differ from source")
	f.BoolVar(&dryRun, "dry-run", false, "Show what would be done without making any changes")
	f.IntVar(&concurrency, "concurrency", config.DefaultConcurrency, "Number of concurrent transfers")
	f.IntVar(&partSizeMB, "part-size", config.DefaultPartSizeMB, "Multipart part size in MB (cross-endpoint only) (default: alias multipart_chunk_size_mb or 15)")
	f.Int64Var(&sizeLimit, "size-limit", 0, "Skip objects larger than N bytes (0 = no limit)")
	f.IntVar(&maxDelete, "max-delete", 0, "Abort before deleting more than N target objects (0 = no limit)")
	f.StringSliceVar(&include, "include", nil, "Only sync keys matching this glob (can repeat)")
	f.StringSliceVar(&exclude, "exclude", nil, "Skip keys matching this glob (can repeat)")
	f.StringVar(&manifest, "manifest", "", "Append successful copied keys to this manifest file")
	f.BoolVar(&resume, "resume", false, "Skip keys already recorded in --manifest")
	return cmd
}
