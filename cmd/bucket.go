package cmd

import (
	"s3cli/internal/action"
	"s3cli/internal/s3path"

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
	)
	return cmd
}

// CreateBucketCmd 创建存储桶
func CreateBucketCmd() *cobra.Command {
	var mkOpt action.MakeBucketOptions
	cmd := &cobra.Command{
		Use:   "create [alias:bucket] ...",
		Short: "Create bucket",
		Args:  cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, _ *Context, s3path *s3path.Path) error {
			return S3.MakeBuckets(mkOpt, s3path.Bucket)
		}, new(newCmdContext())),
	}
	cmd.Flags().StringVar(&mkOpt.CorsFile, "set-cors", "", "cors-file")
	cmd.Flags().StringVar(&mkOpt.LifecycleFile, "set-lifecycle", "", "lifecycle-file")
	cmd.Flags().StringVar(&mkOpt.PolicyFile, "set-policy", "", "policy-file")
	cmd.Flags().BoolVar(&mkOpt.Versioning, "versioning", false, "Enable versioning for the bucket")
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
		RunE: NewRunE(func(S3 action.Action, opts *Context, s3path *s3path.Path) error {
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
		Short: "Manage CORS configuration for bucket",
	}
	corsCmd.AddCommand(CorsSetCmd(), CorsGetCmd(), CorsDelCmd())
	return corsCmd
}

func CorsSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "set [cors-file] [alias:bucket] ...",
		Short:             "Set CORS rules for bucket (xml or json, AWS CLI compatible)",
		ValidArgsFunction: CompleteLocalFirst(AutoCompleteBucket),
		Args:              cobra.MinimumNArgs(2),
		RunE: NewRunE(func(S3 action.Action, opts *Context, s3path *s3path.Path) error {
			return S3.SetCors(opts.Global.Args, s3path.Bucket)
		}, &Context{ArgParseMode: ParseArgsAndS3Path}),
	}
}

func CorsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "get [alias:bucket] ...",
		Short:             "Print CORS rules of bucket(s) as JSON",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, _ *Context, s3path *s3path.Path) error {
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
		RunE: NewRunE(func(S3 action.Action, _ *Context, s3path *s3path.Path) error {
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
	var opt action.LifecycleOptions
	cmd := &cobra.Command{
		Use:               "set [alias:bucket] ...",
		Short:             "Set lifecycle rules (by --prefix/--ttl flags or JSON/XML file)",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, _ *Context, s3path *s3path.Path) error {
			return S3.SetLifecycle(opt, s3path.Bucket)
		}, nil),
	}
	cmd.Flags().StringVar(&opt.Prefix, "prefix", "", "Object key prefix the expiration rule applies to")
	cmd.Flags().StringVar(&opt.TTL, "ttl", "", "Expiration TTL, e.g. 30d / 12h / 1w / 2m (bare number = days)")
	cmd.Flags().StringVar(&opt.ConfigFile, "from-file", "", "Set lifecycle from a JSON/XML file (overrides --prefix/--ttl)")
	return cmd
}

func LifecycleGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "get [alias:bucket] ...",
		Short:             "Print current lifecycle rules (JSON)",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, _ *Context, s3path *s3path.Path) error {
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
		RunE: NewRunE(func(S3 action.Action, _ *Context, s3path *s3path.Path) error {
			return S3.DelLifecycle(s3path.Bucket)
		}, nil),
	}
}

// PolicyCmd 管理桶的访问策略。
// set 统一入口: --type 套用预定义策略 (public-read 等, 可选 --prefix 限定范围),
// 或 -f 指定自定义 JSON 文件; get/del 读取/删除策略。
func PolicyCmd() *cobra.Command {
	policyCmd := &cobra.Command{
		Use:   "policy",
		Short: "Manage bucket policy",
	}
	policyCmd.AddCommand(
		PolicySetCmd(),
		PolicyGetCmd(),
		PolicyDelCmd(),
	)
	return policyCmd
}

// PolicySetCmd 设置桶策略的统一入口: --type 预定义策略 或 -f 自定义 JSON 文件.
func PolicySetCmd() *cobra.Command {
	var opt action.PolicyOptions
	cmd := &cobra.Command{
		Use:               "set [alias:bucket] ...",
		Short:             "Set bucket policy (canned type or custom JSON file)",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, _ *Context, s3path *s3path.Path) error {
			return S3.SetPolicy(opt, s3path.Bucket)
		}, nil),
	}
	cmd.Flags().StringVar(&opt.Type, "type", "", "Canned policy: public-read / public-write / public-read-write / private")
	cmd.Flags().StringVar(&opt.Prefix, "prefix", "", "Scope canned policy to objects under this key prefix (default: whole bucket)")
	cmd.Flags().StringVar(&opt.ConfigFile, "from-file", "", "Set policy from a custom JSON file (overrides --type)")
	return cmd
}

func PolicyGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "get [alias:bucket] ...",
		Short:             "Print current bucket policy (pretty JSON)",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, _ *Context, s3path *s3path.Path) error {
			return S3.GetPolicy(s3path.Bucket)
		}, nil),
	}
}

func PolicyDelCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "del [alias:bucket] ...",
		Short:             "Delete bucket policy",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, _ *Context, s3path *s3path.Path) error {
			return S3.DelPolicy(s3path.Bucket)
		}, nil),
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
		Short:             "Set bucket event notifications (SQS/SNS/Lambda, JSON, AWS CLI compatible)",
		ValidArgsFunction: CompleteLocalFirst(AutoCompleteBucket),
		Args:              cobra.MinimumNArgs(2),
		RunE: NewRunE(func(S3 action.Action, opts *Context, s3path *s3path.Path) error {
			return S3.SetNotification(opts.Global.Args, s3path.Bucket)
		}, &Context{ArgParseMode: ParseArgsAndS3Path}),
	}
}

func EventGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "get [alias:bucket] ...",
		Short:             "Print bucket(s) event notification configuration (JSON)",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, _ *Context, s3path *s3path.Path) error {
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
		RunE: NewRunE(func(S3 action.Action, _ *Context, s3path *s3path.Path) error {
			return S3.DelNotification(s3path.Bucket)
		}, nil),
	}
}

// EncryptionCmd 管理桶的默认加密配置 (SSE-S3 / SSE-KMS)
func EncryptionCmd() *cobra.Command {
	encryptionCmd := &cobra.Command{
		Use:   "encryption",
		Short: "Manage bucket default encryption (SSE-S3 / SSE-KMS)",
	}
	encryptionCmd.AddCommand(EncryptionSetCmd(), EncryptionGetCmd(), EncryptionDelCmd())
	return encryptionCmd
}

func EncryptionSetCmd() *cobra.Command {
	var encOpt action.EncryptionOptions
	cmd := &cobra.Command{
		Use:               "set [alias:bucket] ...",
		Short:             "Set bucket default encryption (SSE-S3 / SSE-KMS)",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, _ *Context, s3path *s3path.Path) error {
			return S3.SetEncryption(encOpt, s3path.Bucket)
		}, nil),
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
		RunE: NewRunE(func(S3 action.Action, _ *Context, s3path *s3path.Path) error {
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
		RunE: NewRunE(func(S3 action.Action, _ *Context, s3path *s3path.Path) error {
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
		RunE: NewRunE(func(S3 action.Action, _ *Context, s3path *s3path.Path) error {
			return S3.SetVersioning(s3path.Bucket, set)
		}, nil),
	}

	// --status 选项必须指定
	// 兼容 MinIO 和 AWS 在关闭版本控制上不同的行为
	// MinIO 使用Suspended，AWS使用Disabled
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
		RunE: NewRunE(func(S3 action.Action, _ *Context, s3path *s3path.Path) error {
			return S3.GetVersioning(s3path.Bucket)
		}, nil),
	}
}
