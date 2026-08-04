package cmd

import (
	"fmt"
	"s3cli/internal/s3path"
	"strings"

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

// copyMoveAction 同 endpoint 复制/移动动作的公共签名。
type copyMoveAction func(src action.Action, opt action.CopyOptions, srcPath, dstPath *s3path.Path) error

// copyMoveSpec cp/mv 的差异点 (动作、文案), 其余共用同一构造器。
type copyMoveSpec struct {
	Use        string
	Long       string
	Short      string
	Verb       string // 错误文案中的动词: copy / move
	VerbPast   string // 帮助文案中的过去分词: copied / moved
	MirrorHint string // 跨 endpoint 时的提示
	Fn         copyMoveAction
}

// newCopyMoveCmd 构造 cp/mv 的公共实现 (除动作与文案外完全相同)。
func newCopyMoveCmd(spec copyMoveSpec) *cobra.Command {
	var opt action.CopyOptions
	cmd := &cobra.Command{
		Use:               spec.Use,
		Long:              spec.Long,
		Short:             spec.Short,
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: AutoCompletePath,
		RunE: NewRunETwoPaths(func(src, dst action.Action, srcPath, dstPath *s3path.Path) error {
			if srcPath.Alias != dstPath.Alias {
				return fmt.Errorf("%s only supports same-alias %s; %s", spec.Verb, spec.Verb, spec.MirrorHint)
			}
			if samePath(srcPath, dstPath) {
				return fmt.Errorf("source and destination are the same: %s", action.S3PathStatic(srcPath.Alias, srcPath.Bucket, srcPath.Key))
			}
			return spec.Fn(src, opt, srcPath, dstPath)
		}),
	}
	f := cmd.Flags()
	f.BoolVarP(&opt.Recursive, "recursive", "r", false,
		fmt.Sprintf("%s%s recursively", strings.ToUpper(spec.Verb[:1]), spec.Verb[1:]))
	f.StringVar(&opt.StorageClass, "storage-class", "", fmt.Sprintf("Storage class for the %s object(s)", spec.VerbPast))
	f.StringVar(&opt.StorageClass, "sc", "", "Alias of --storage-class")
	f.StringVar(&opt.Tags, "tags", "", fmt.Sprintf("Apply tags to the %s object(s): '<key1>=<value1>&<key2>=<value2>'", spec.VerbPast))
	f.StringToStringVar(&opt.Metadata, "metadata", nil, "Replace object metadata (x-amz-meta-*). Can repeat. Format: key=value")
	f.BoolVarP(&opt.NoProgress, "quiet", "q", false, "Disable progress bar; stream plain text output instead")
	return cmd
}

func NewCpCmd() *cobra.Command {
	return newCopyMoveCmd(copyMoveSpec{
		Use:        "cp [src-alias:bucket/key] [dst-alias:bucket/key]",
		Long:       "Copy object within the same S3 endpoint (text output only)",
		Short:      "Copy object within the same S3 endpoint",
		Verb:       "copy",
		VerbPast:   "copied",
		MirrorHint: "use `mirror` for cross-endpoint",
		Fn: func(src action.Action, opt action.CopyOptions, srcPath, dstPath *s3path.Path) error {
			return src.CopyObjects(opt, srcPath.Bucket, srcPath.Key, dstPath.Bucket, dstPath.Key)
		},
	})
}

func NewMvCmd() *cobra.Command {
	return newCopyMoveCmd(copyMoveSpec{
		Use:        "mv [src-alias:bucket/key] [dst-alias:bucket/key]",
		Long:       "Move object within the same S3 endpoint (text output only)",
		Short:      "Move object within the same S3 endpoint",
		Verb:       "move",
		VerbPast:   "moved",
		MirrorHint: "use `mirror --remove` for cross-endpoint",
		Fn: func(src action.Action, opt action.CopyOptions, srcPath, dstPath *s3path.Path) error {
			return src.Mv(opt, srcPath.Bucket, srcPath.Key, dstPath.Bucket, dstPath.Key)
		},
	})
}

func NewMirrorCmd() *cobra.Command {
	var opt action.MirrorOptions

	// cmd 先声明后赋值: RunE 闭包需要引用 cmd 判断 --part-size 是否被显式设置
	var cmd *cobra.Command
	cmd = &cobra.Command{
		Use:               "mirror [src-alias:bucket/prefix] [dst-alias:bucket/prefix]",
		Long:              "Synchronize objects from source to target (text output only)",
		Aliases:           []string{"sync"},
		Short:             "Synchronize objects from source to target (one-way sync)",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: AutoCompletePath,
		RunE: NewRunETwoPaths(func(src, tgt action.Action, srcPath, tgtPath *s3path.Path) error {
			if srcPath.Bucket == "" || tgtPath.Bucket == "" {
				return fmt.Errorf("both src and dst must include a bucket")
			}
			if srcPath.Alias == tgtPath.Alias {
				return fmt.Errorf("mirror requires different aliases (use `cp` for same-alias copy)")
			}

			// alias 配置的 chunk size 仅在 --part-size 未显式设置时生效;
			// 跨端 MPU 在目标端创建, 故取目标 alias 的配置。
			if cfg, ok := config.G.S[tgtPath.Alias]; ok && cfg.MultipartChunkSizeMb > 0 && !cmd.Flags().Changed("part-size") {
				opt.PartSizeMB = cfg.MultipartChunkSizeMb
			}

			opt.Src = &action.S3PathOptions{
				Client:        &src,
				Bucket:        srcPath.Bucket,
				ObjectKey:     srcPath.Key,
				TrailingSlash: srcPath.TrailingSlash,
			}
			opt.Tgt = &action.S3PathOptions{
				Client:        &tgt,
				Bucket:        tgtPath.Bucket,
				ObjectKey:     tgtPath.Key,
				TrailingSlash: tgtPath.TrailingSlash,
			}
			return action.Mirror(opt)
		}),
	}

	f := cmd.Flags()
	f.BoolVar(&opt.Remove, "remove", false, "Delete extra objects on target that don't exist on source")
	f.BoolVar(&opt.Overwrite, "overwrite", false, "Overwrite target objects whose ETag/size/mtime differ from source")
	f.BoolVar(&opt.DryRun, "dry-run", false, "Show what would be done without making any changes")
	f.IntVar(&opt.Concurrency, "concurrency", config.DefaultConcurrency, "Number of concurrent transfers")
	f.IntVar(&opt.PartSizeMB, "part-size", config.DefaultPartSizeMB, "Multipart part size in MB (cross-endpoint only) (default: alias multipart_chunk_size_mb or 15)")
	f.Int64Var(&opt.SizeLimit, "size-limit", 0, "Skip objects larger than N bytes (0 = no limit)")
	f.IntVar(&opt.MaxDelete, "max-delete", 0, "Abort before deleting more than N target objects (0 = no limit)")
	f.StringSliceVar(&opt.Include, "include", nil, "Only sync keys matching this glob (can repeat)")
	f.StringSliceVar(&opt.Exclude, "exclude", nil, "Skip keys matching this glob (can repeat)")
	f.StringVar(&opt.ManifestPath, "manifest", "", "Append successful copied keys to this manifest file")
	f.BoolVar(&opt.Resume, "resume", false, "Skip keys already recorded in --manifest")
	f.BoolVarP(&opt.NoProgress, "quiet", "q", false, "Disable progress bar; stream plain text output instead")
	return cmd
}
