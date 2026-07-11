package cmd

import (
	"s3cli/pkg/action"
	"s3cli/pkg/utils"

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
		Short:   "Bucket management and configuration",
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
		QuotaCmd(),
	)
	return cmd
}

// CreateBucketCmd 创建存储桶
func CreateBucketCmd() *cobra.Command {
	var mkOpt action.MakeBucketOptions
	opts := newCmdContext()
	cmd := &cobra.Command{
		Use:   "create [alias:bucket] ...",
		Short: "Create new bucket",
		Args:  cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.S3Client, _ *Context, s3path *utils.S3Path) error {
			return S3.MakeBuckets(mkOpt, s3path.Bucket)
		}, &opts),
	}
	cmd.Flags().StringVar(&mkOpt.CorsFile, "set-cors", "", "cors-file")
	cmd.Flags().StringVar(&mkOpt.LifecycleFile, "set-lifecycle", "", "lifecycle-file")
	cmd.Flags().StringVar(&mkOpt.PolicyFile, "set-policy", "", "policy-file")
	cmd.Flags().BoolVar(&mkOpt.Versioning, "versioning", false, "Enable versioning for the bucket")
	cmd.Flags().StringVar(&mkOpt.Quota, "quota", "", "Set bucket quota (e.g. 10GB, 100MB), Only supported MinIO server")
	cmd.ValidArgsFunction = AutoCompleteBucket
	return cmd
}

// RemoveBucketCmd 删除存储桶
func RemoveBucketCmd() *cobra.Command {
	opts := newCmdContext()
	cmd := &cobra.Command{
		Use:   "remove [alias:bucket] ...",
		Short: "Remove bucket",
		Args:  cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.S3Client, opts *Context, s3path *utils.S3Path) error {
			return S3.RemoveBuckets(s3path.Bucket, opts.Global.Force)
		}, &opts),
	}
	cmd.Flags().BoolVar(&opts.Global.Force, "force", false, "Force remove bucket even if not empty")
	cmd.ValidArgsFunction = AutoCompleteBucket
	return cmd
}

// CorsCmd 管理桶的 CORS 配置
func CorsCmd() *cobra.Command {
	corsCmd := &cobra.Command{
		Use:   "cors",
		Short: "Manage CORS configuration for bucket(s)",
	}
	corsCmd.AddCommand(CorsSetCmd(), CorsGetCmd(), CorsDelCmd())
	return corsCmd
}

func CorsSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "set [cors-file] [alias:bucket] ...",
		Short:             "Set CORS rules for bucket (xml or json, AWS CLI compatible)",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(2),
		RunE: NewRunE(func(S3 action.S3Client, opts *Context, s3path *utils.S3Path) error {
			return S3.SetCors(opts.Global.LocalFile, s3path.Bucket)
		}, &Context{ArgParseMode: ParseLocalFileAndS3Path}),
	}
}

func CorsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "get [alias:bucket] ...",
		Short:             "Print CORS rules of bucket(s) as JSON",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.S3Client, _ *Context, s3path *utils.S3Path) error {
			return S3.GetCors(s3path.Bucket)
		}, nil),
	}
}

func CorsDelCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "del [alias:bucket] ...",
		Short:             "Delete CORS rules for bucket(s)",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.S3Client, _ *Context, s3path *utils.S3Path) error {
			return S3.DelCors(s3path.Bucket)
		}, nil),
	}
}

// LifecycleCmd 管理桶的生命周期规则
func LifecycleCmd() *cobra.Command {
	lifecycleCmd := &cobra.Command{
		Use:   "lifecycle",
		Short: "Manage lifecycle rules",
	}
	lifecycleCmd.AddCommand(SetLifecycleSetCmd(), LifecycleGetCmd(), LifecycleDelCmd())
	return lifecycleCmd
}

func SetLifecycleSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "set [lifecycle-file] [alias:bucket] ...",
		Short:             "Set lifecycle rules (JSON, AWS CLI compatible)",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(2),
		RunE: NewRunE(func(S3 action.S3Client, opts *Context, s3path *utils.S3Path) error {
			return S3.SetLifecycle(opts.Global.LocalFile, s3path.Bucket)
		}, &Context{ArgParseMode: ParseLocalFileAndS3Path}),
	}
}

func LifecycleGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "get [alias:bucket] ...",
		Short:             "Print current lifecycle rules (JSON)",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.S3Client, _ *Context, s3path *utils.S3Path) error {
			return S3.GetLifecycle(s3path.Bucket)
		}, nil),
	}
}

func LifecycleDelCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "del [alias:bucket] ...",
		Short:             "Delete all lifecycle rules",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.S3Client, _ *Context, s3path *utils.S3Path) error {
			return S3.DelLifecycle(s3path.Bucket)
		}, nil),
	}
}

// PolicyCmd 管理桶的访问策略
func PolicyCmd() *cobra.Command {
	policyCmd := &cobra.Command{
		Use:   "policy",
		Short: "Manage bucket policy",
	}
	policyCmd.AddCommand(PolicySetCmd(), PolicyGetCmd(), PolicyDelCmd())
	return policyCmd
}

func PolicySetCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "set [policy-file] [alias:bucket] ...",
		Short:             "Set bucket policy (JSON)",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(2),
		RunE: NewRunE(func(S3 action.S3Client, opts *Context, s3path *utils.S3Path) error {
			return S3.SetPolicy(opts.Global.LocalFile, s3path.Bucket)
		}, &Context{ArgParseMode: ParseLocalFileAndS3Path}),
	}
}

func PolicyGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "get [alias:bucket] ...",
		Short:             "Print current bucket policy (pretty JSON)",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.S3Client, _ *Context, s3path *utils.S3Path) error {
			return S3.GetPolicy(s3path.Bucket)
		}, &Context{}),
	}
}

func PolicyDelCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "del [alias:bucket] ...",
		Short:             "Delete bucket policy",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.S3Client, _ *Context, s3path *utils.S3Path) error {
			return S3.DelPolicy(s3path.Bucket)
		}, &Context{}),
	}
}

// EventCmd 管理桶的事件通知配置
func EventCmd() *cobra.Command {
	eventCmd := &cobra.Command{
		Use:   "event",
		Short: "Manage object notifications",
	}
	eventCmd.AddCommand(EventSetCmd(), EventGetCmd(), EventDelCmd())
	return eventCmd
}

func EventSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "set [notification-file] [alias:bucket] ...",
		Short:             "Set bucket(s) event notifications (SQS/SNS/Lambda, JSON, AWS CLI compatible)",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(2),
		RunE: NewRunE(func(S3 action.S3Client, opts *Context, s3path *utils.S3Path) error {
			return S3.SetNotification(opts.Global.LocalFile, s3path.Bucket)
		}, &Context{ArgParseMode: ParseLocalFileAndS3Path}),
	}
}

func EventGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "get [alias:bucket] ...",
		Short:             "Print bucket(s) event notification configuration (JSON)",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.S3Client, _ *Context, s3path *utils.S3Path) error {
			return S3.GetNotification(s3path.Bucket)
		}, nil),
	}
}

func EventDelCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "del [alias:bucket] ...",
		Short:             "Remove all bucket event notification configurations",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.S3Client, _ *Context, s3path *utils.S3Path) error {
			return S3.DelNotification(s3path.Bucket)
		}, nil),
	}
}

// EncryptionCmd 管理桶的默认加密配置 (SSE-S3 / SSE-KMS)
func EncryptionCmd() *cobra.Command {
	encryptionCmd := &cobra.Command{
		Use:   "encryption",
		Short: "Manage bucket(s) default encryption (SSE-S3 / SSE-KMS)",
	}
	encryptionCmd.AddCommand(EncryptionSetCmd(), EncryptionGetCmd(), EncryptionDelCmd())
	return encryptionCmd
}

func EncryptionSetCmd() *cobra.Command {
	var encOpt action.EncryptionOptions
	opts := newCmdContext()
	cmd := &cobra.Command{
		Use:               "set [alias:bucket] ...",
		Short:             "Set bucket(s) default encryption (SSE-S3 / SSE-KMS)",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.S3Client, _ *Context, s3path *utils.S3Path) error {
			return S3.SetEncryption(encOpt, s3path.Bucket)
		}, &opts),
	}

	cmd.Flags().StringVar(&encOpt.Algorithm, "algorithm", "AES256", "Encryption algorithm: AES256 / aws:kms")
	cmd.Flags().StringVar(&encOpt.KMSKeyID, "kms-key-id", "", "KMS key id (required for aws:kms)")
	cmd.Flags().BoolVar(&encOpt.BucketKey, "bucket-key", false, "Enable S3 Bucket Key (aws:kms only)")
	cmd.Flags().StringVar(&encOpt.ConfigFile, "from-file", "", "Load AWS CLI JSON config instead of using flags")

	return cmd
}

func EncryptionGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "get [alias:bucket] ...",
		Short:             "Print bucket(s) default encryption configuration (JSON)",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.S3Client, _ *Context, s3path *utils.S3Path) error {
			return S3.GetEncryption(s3path.Bucket)
		}, nil),
	}
}

func EncryptionDelCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "del [alias:bucket] ...",
		Short:             "Delete bucket(s) default encryption configuration",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.S3Client, _ *Context, s3path *utils.S3Path) error {
			return S3.DelEncryption(s3path.Bucket)
		}, nil),
	}
}

// VersioningCmd 管理桶的版本控制配置
func VersioningCmd() *cobra.Command {
	versionCmd := &cobra.Command{
		Use:   "versioning",
		Short: "Manage bucket versioning",
	}

	versionCmd.AddCommand(VersioningSetCmd(), VersioningInfoCmd())
	return versionCmd
}

func VersioningSetCmd() *cobra.Command {
	var set string
	cmd := &cobra.Command{
		Use:               "set [alias:bucket] ...",
		Short:             "Set bucket versioning status",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.S3Client, _ *Context, s3path *utils.S3Path) error {
			return S3.SetVersioning(s3path.Bucket, set)
		}, nil),
	}

	cmd.Flags().StringVar(&set, "status", "", "Set bucket versioning status")
	_ = cmd.MarkFlagRequired("status")
	return cmd
}

func VersioningInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "info [alias:bucket] ...",
		Short:             "Print current versioning status",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.S3Client, _ *Context, s3path *utils.S3Path) error {
			return S3.GetVersioning(s3path.Bucket)
		}, nil),
	}
}

// QuotaCmd 配置存储桶 Quota (仅支持MinIO服务器)
func QuotaCmd() *cobra.Command {
	quotaCmd := &cobra.Command{
		Use:   "quota",
		Short: "Manage bucket quota (Only supported MinIO server)",
	}
	quotaCmd.AddCommand(QuotaSetCmd(), QuotaInfoCmd(), QuotaClearCmd())
	return quotaCmd
}

func QuotaSetCmd() *cobra.Command {
	var quota string
	cmd := &cobra.Command{
		Use:               "set quotaStr [alias:bucket] ...",
		Short:             "Set bucket quota (e.g. 10GB, 100MB), Only supported MinIO server",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(2),
		RunE: NewRunE(func(S3 action.S3Client, _ *Context, s3path *utils.S3Path) error {
			return S3.SetBucketQuota(s3path.Bucket, quota)
		}, nil),
	}
	return cmd
}

func QuotaInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "info [alias:bucket] ...",
		Short:             "Print current bucket quota (Only supported MinIO server)",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.S3Client, _ *Context, s3path *utils.S3Path) error {
			return S3.InfoBucketQuota(s3path.Bucket)
		}, nil),
	}
}

func QuotaClearCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "clear [alias:bucket] ...",
		Short:             "Clear bucket quota (Only supported MinIO server)",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.S3Client, _ *Context, s3path *utils.S3Path) error {
			return S3.SetBucketQuota(s3path.Bucket, "0")
		}, nil),
	}
}
