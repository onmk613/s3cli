package cmd

import (
	"s3cli/internal/action"
	"s3cli/internal/s3path"
	"s3cli/pkg/i18n"

	"github.com/spf13/cobra"
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
	)
	return cmd
}

// CreateBucketCmd 创建存储桶
func CreateBucketCmd() *cobra.Command {
	var mkOpt action.MakeBucketOptions
	cmd := &cobra.Command{
		Use:               "create [alias:bucket] ...",
		Short:             i18n.T("Create bucket", "创建存储桶"),
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

// RemoveBucketCmd 删除存储桶
func RemoveBucketCmd() *cobra.Command {
	var opts action.RemoveBucketOptions
	cmd := &cobra.Command{
		Use:               "remove [alias:bucket] ...",
		Short:             i18n.T("Remove bucket", "删除存储桶"),
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: AutoCompleteBucket,
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.RemoveBuckets(opts, dst.Bucket)
		}),
	}
	cmd.Flags().BoolVar(&opts.Force, "force", false, i18n.T("Force remove bucket even if not empty", "即使桶不为空也强制删除"))
	return cmd
}
