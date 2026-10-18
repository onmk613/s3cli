package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/onmk613/s3cli/internal/action"
	"github.com/onmk613/s3cli/internal/i18n"
	"github.com/onmk613/s3cli/internal/s3path"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func init() {
	Register("bucket", "Bucket Commands", NewBucketCmd)
}

// NewBucketCmd 所有桶级配置命令的父命令, 统一挂载到 "bucket" 命令下.
func NewBucketCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "bucket",
		Aliases: []string{"b"},
		Short:   i18n.T("Bucket management and configuration", "存储桶管理与配置"),
	}
	cmd.AddCommand(
		CreateBucketCmd(),
		RemoveBucketCmd(),
		CorsCmd(),
		LifecycleCmd(),
		PolicyCmd(),
		EventCmd(),
		EncryptionCmd(),
		VersioningCmd(),
		BucketACLCmd(),
		ObjectLockCmd(),
		ReplicationCmd(),
		PublicAccessBlockCmd(),
	)
	return cmd
}

// CreateBucketCmd 创建存储桶
func CreateBucketCmd() *cobra.Command {
	var mkOpt action.MakeBucketOptions
	cmd := &cobra.Command{
		Use:               "create [alias:bucket] ...",
		Short:             i18n.T("Create bucket", "创建存储桶"),
		Long:              i18n.T("Create bucket.\n\nFor buckets created with --with-lock, the default retention rule is configured with `s3cli bucket object-lock set`.", "创建存储桶。\n\n对使用 --with-lock 创建的存储桶，默认保留策略通过 `s3cli bucket object-lock set` 配置。"),
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: AutoCompleteBucket,
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.MakeBuckets(mkOpt, dst.Bucket)
		}),
	}
	cmd.Flags().StringVar(&mkOpt.CorsFile, "set-cors", "", i18n.T("cors-file", "cors 配置文件"))
	cmd.Flags().StringVar(&mkOpt.LifecycleFile, "set-lifecycle", "", i18n.T("lifecycle-file", "lifecycle 配置文件"))
	cmd.Flags().StringVar(&mkOpt.PolicyFile, "set-policy", "", i18n.T("policy-file", "policy 配置文件"))
	cmd.Flags().BoolVar(&mkOpt.Versioning, "versioning", false, i18n.T("Enable versioning for the bucket", "为存储桶启用版本控制"))
	cmd.Flags().StringVar(&mkOpt.Region, "region", "", i18n.T("Specify bucket region (default: us-east-1)", "指定存储桶 region（默认：us-east-1）"))
	cmd.Flags().BoolVarP(&mkOpt.ObjectLocking, "with-lock", "l", false, i18n.T("Enable object lock on the bucket", "为存储桶启用对象锁"))
	cmd.Flags().BoolVarP(&mkOpt.IgnoreExisting, "ignore-existing", "p", false, i18n.T("Ignore if the bucket already exists", "存储桶已存在时忽略（不报错）"))
	return cmd
}

// RemoveBucketCmd 删除存储桶。
//
// --force 会先清空桶内全部对象与版本, 属于不可恢复操作。在终端下额外要求
// 手输桶名确认 (--yes 可跳过), 避免复制粘贴/手滑把生产桶清空; 非终端
// (CI/脚本) 保持原行为, 由 --force 作为显式意图。
func RemoveBucketCmd() *cobra.Command {
	var (
		opts action.RemoveBucketOptions
		yes  bool
	)
	cmd := &cobra.Command{
		Use:   "remove [alias:bucket] ...",
		Short: i18n.T("Remove bucket", "删除存储桶"),
		Long: i18n.T(
			"Remove a bucket.\n\n"+
				"--force also deletes every object/version first (irreversible). On a\n"+
				"terminal --force additionally asks you to type the bucket name; use\n"+
				"--yes to skip that prompt in scripts.",
			"删除存储桶。\n\n"+
				"--force 会先清空桶内全部对象与版本（不可恢复）。在终端下 --force 还会\n"+
				"要求手动输入桶名确认；脚本可用 --yes 跳过。"),
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: AutoCompleteBucket,
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			if opts.Force && !opts.DryRun && !yes && term.IsTerminal(int(os.Stdin.Fd())) {
				if !confirmBucketRemoval(dst.Bucket, os.Stdin, os.Stderr) {
					return fmt.Errorf(i18n.T("bucket removal aborted: confirmation did not match %q", "已取消删除存储桶：确认输入与 %q 不一致"), dst.Bucket)
				}
			}
			return S3.RemoveBuckets(opts, dst.Bucket)
		}),
	}
	cmd.Flags().BoolVar(&opts.Force, "force", false, i18n.T("Force remove bucket even if not empty", "即使桶不为空也强制删除"))
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, i18n.T("Skip the interactive confirmation required by --force on a terminal", "跳过终端下 --force 的交互确认"))
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, i18n.T("Show what would be deleted without removing anything", "只显示将删除的内容，不做任何删除"))
	return cmd
}

// confirmBucketRemoval 要求用户输入完整桶名; 仅完全匹配才返回 true。
// 独立成函数便于在非 TTY 测试里直接验证输入判定。
func confirmBucketRemoval(bucket string, in io.Reader, out io.Writer) bool {
	_, _ = fmt.Fprintf(out, i18n.T(
		"Type the bucket name (%s) to confirm permanent deletion: ",
		"请输入存储桶名称（%s）以确认永久删除："), bucket)
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && line == "" {
		return false
	}
	return strings.TrimSpace(line) == bucket
}
