package cmd

import (
	"fmt"

	"s3cli/internal/action"
	"s3cli/internal/config"
	"s3cli/internal/s3path"
	"s3cli/pkg/i18n"

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
	Use           string
	Long          string
	Short         string
	RecursiveDesc string
	StorageDesc   string
	TagsDesc      string
	Verb          string // 错误文案中的动词: copy / move
	MirrorHint    string // 跨 endpoint 时的提示
	Fn            copyMoveAction
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
	f.BoolVarP(&opt.Recursive, "recursive", "r", false, spec.RecursiveDesc)
	f.StringVar(&opt.StorageClass, "storage-class", "", spec.StorageDesc)
	f.StringVar(&opt.StorageClass, "sc", "", scAliasDesc())
	f.StringVar(&opt.Tags, "tags", "", spec.TagsDesc)
	f.StringToStringVar(&opt.Metadata, "metadata", nil, i18n.T("Replace object metadata (x-amz-meta-*). Can repeat. Format: key=value", "替换对象元数据（x-amz-meta-*）。可重复。格式：key=value"))
	f.BoolVarP(&opt.NoProgress, "quiet", "q", false, quietDesc())
	return cmd
}

func NewCpCmd() *cobra.Command {
	return newCopyMoveCmd(copyMoveSpec{
		Use:           "cp [src-alias:bucket/key] [dst-alias:bucket/key]",
		Long:          i18n.T("Copy object within the same S3 endpoint (text output only)", "在同一 S3 endpoint 内复制对象（仅文本输出）"),
		Short:         i18n.T("Copy object within the same S3 endpoint", "在同一 S3 endpoint 内复制对象"),
		RecursiveDesc: i18n.T("Copy recursively", "递归复制"),
		StorageDesc:   i18n.T("Storage class for the copied object(s)", "被复制对象的存储类型"),
		TagsDesc:      i18n.T("Apply tags to the copied object(s): '<key1>=<value1>&<key2>=<value2>'", "给被复制对象添加标签：'<key1>=<value1>&<key2>=<value2>'"),
		Verb:          "copy",
		MirrorHint:    "use `mirror` for cross-endpoint",
		Fn: func(src action.Action, opt action.CopyOptions, srcPath, dstPath *s3path.Path) error {
			return src.CopyObjects(opt, srcPath.Bucket, srcPath.Key, dstPath.Bucket, dstPath.Key)
		},
	})
}

func NewMvCmd() *cobra.Command {
	return newCopyMoveCmd(copyMoveSpec{
		Use:           "mv [src-alias:bucket/key] [dst-alias:bucket/key]",
		Long:          i18n.T("Move object within the same S3 endpoint (text output only)", "在同一 S3 endpoint 内移动对象（仅文本输出）"),
		Short:         i18n.T("Move object within the same S3 endpoint", "在同一 S3 endpoint 内移动对象"),
		RecursiveDesc: i18n.T("Move recursively", "递归移动"),
		StorageDesc:   i18n.T("Storage class for the moved object(s)", "被移动对象的存储类型"),
		TagsDesc:      i18n.T("Apply tags to the moved object(s): '<key1>=<value1>&<key2>=<value2>'", "给被移动对象添加标签：'<key1>=<value1>&<key2>=<value2>'"),
		Verb:          "move",
		MirrorHint:    "use `mirror --remove` for cross-endpoint",
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
		Long:              i18n.T("Synchronize objects from source to target (text output only)", "把对象从源同步到目标（仅文本输出）"),
		Aliases:           []string{"sync"},
		Short:             i18n.T("Synchronize objects from source to target (one-way sync)", "把对象从源单向同步到目标"),
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
	f.BoolVar(&opt.Remove, "remove", false, i18n.T("Delete extra objects on target that don't exist on source", "删除目标端多出的、源端不存在的对象"))
	f.BoolVar(&opt.Overwrite, "overwrite", false, i18n.T("Overwrite target objects whose ETag/size/mtime differ from source", "覆盖目标端 ETag/大小/mtime 与源不一致的对象"))
	f.BoolVar(&opt.DryRun, "dry-run", false, i18n.T("Show what would be done without making any changes", "只显示将执行的操作，不做任何实际变更"))
	f.IntVar(&opt.Concurrency, "concurrency", config.DefaultConcurrency, i18n.T("Number of concurrent transfers", "并发传输数"))
	f.IntVar(&opt.PartSizeMB, "part-size", config.DefaultPartSizeMB, i18n.T("Multipart part size in MB (cross-endpoint only) (default: alias multipart_chunk_size_mb or 15)", "分段上传的分段大小（MB）（仅跨 endpoint）（默认：alias 的 multipart_chunk_size_mb 或 15）"))
	f.StringVar(&opt.StorageClass, "storage-class", "", i18n.T("Storage class for target objects: STANDARD / STANDARD_IA / GLACIER / DEEP_ARCHIVE / ...", "目标对象的存储类型：STANDARD / STANDARD_IA / GLACIER / DEEP_ARCHIVE / ..."))
	f.StringVar(&opt.StorageClass, "sc", "", scAliasDesc())
	f.Int64Var(&opt.SizeLimit, "size-limit", 0, i18n.T("Skip objects larger than N bytes (0 = no limit)", "跳过大于 N 字节的对象（0 = 不限制）"))
	f.IntVar(&opt.MaxDelete, "max-delete", 0, i18n.T("Abort before deleting more than N target objects (0 = no limit)", "删除的目标对象超过 N 个时中止（0 = 不限制）"))
	f.StringSliceVar(&opt.Include, "include", nil, i18n.T("Only sync keys matching this glob (can repeat)", "只同步匹配该通配模式的 key（可重复）"))
	f.StringSliceVar(&opt.Exclude, "exclude", nil, i18n.T("Skip keys matching this glob (can repeat)", "跳过匹配该通配模式的 key（可重复）"))
	f.StringVar(&opt.ManifestPath, "manifest", "", i18n.T("Append successful copied keys to this manifest file", "把复制成功的 key 追加记录到该 manifest 文件"))
	f.BoolVar(&opt.Resume, "resume", false, i18n.T("Skip keys already recorded in --manifest", "跳过 --manifest 中已记录的 key"))
	f.BoolVarP(&opt.NoProgress, "quiet", "q", false, quietDesc())
	return cmd
}
