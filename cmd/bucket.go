package cmd

import (
	"fmt"
	"os"

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
		Use:               "create [alias:bucket] ...",
		Short:             "Create bucket",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: AutoCompleteBucket,
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.MakeBuckets(mkOpt, dst.Bucket)
		}),
	}
	cmd.Flags().StringVar(&mkOpt.CorsFile, "set-cors", "", "cors-file")
	cmd.Flags().StringVar(&mkOpt.LifecycleFile, "set-lifecycle", "", "lifecycle-file")
	cmd.Flags().StringVar(&mkOpt.PolicyFile, "set-policy", "", "policy-file")
	cmd.Flags().BoolVar(&mkOpt.Versioning, "versioning", false, "Enable versioning for the bucket")
	cmd.Flags().StringVar(&mkOpt.Region, "region", "", "Specify bucket region (default: us-east-1)")
	cmd.Flags().BoolVarP(&mkOpt.ObjectLocking, "with-lock", "l", false, "Enable object lock on the bucket")
	cmd.Flags().BoolVarP(&mkOpt.IgnoreExisting, "ignore-existing", "p", false, "Ignore if the bucket already exists")
	return cmd
}

// RemoveBucketCmd 删除存储桶
func RemoveBucketCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:               "remove [alias:bucket] ...",
		Short:             "Remove bucket",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: AutoCompleteBucket,
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.RemoveBuckets(dst.Bucket, force)
		}),
	}
	cmd.Flags().BoolVar(&force, "force", false, "Force remove bucket even if not empty")
	return cmd
}

// CorsCmd 管理桶的 CORS 配置 (子命令与 mc cors 对齐: set/get/remove)
func CorsCmd() *cobra.Command {
	corsCmd := &cobra.Command{
		Use:   "cors",
		Short: "Manage CORS configuration for bucket",
	}
	corsCmd.AddCommand(CorsSetCmd(), CorsGetCmd(), CorsRemoveCmd())
	return corsCmd
}

// CorsSetCmd 设置桶 CORS: 参数模式 (--origin/--method/...) 或文件模式
// (-f/--from-file, 也兼容 mc 的位置参数顺序 ALIAS/BUCKET CORSFILE 与旧 FILE ALIAS/BUCKET).
func CorsSetCmd() *cobra.Command {
	var opt action.CorsOptions
	cmd := &cobra.Command{
		Use:               "set [alias:bucket] [cors-file]",
		Short:             "Set CORS rules for bucket (--origin/--method flags or JSON/XML file)",
		ValidArgsFunction: CompleteLocalFirst(AutoCompleteBucket),
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunEWithPreprocess(
			func(_ *cobra.Command, args []string) ([]string, error) {
				var bucketArgs []string
				var fileArg string
				for _, a := range args {
					if fileArg == "" && isLocalFile(a) {
						fileArg = a
						continue
					}
					bucketArgs = append(bucketArgs, a)
				}
				if fileArg == "" && len(args) >= 2 {
					// 两个参数均非本地文件: 按 mc 顺序把最后一个当配置文件
					fileArg = args[len(args)-1]
					bucketArgs = args[:len(args)-1]
				}
				if fileArg != "" {
					opt.ConfigFile = fileArg
				}
				return bucketArgs, nil
			},
			func(S3 action.Action, sp *s3path.Path) error {
				return S3.SetCors(opt, sp.Bucket)
			},
		),
	}
	cmd.Flags().StringVar(&opt.ID, "id", "", "CORS rule ID")
	cmd.Flags().StringArrayVar(&opt.Origins, "origin", nil, "Allowed origin, e.g. https://example.com or * (repeatable)")
	cmd.Flags().StringArrayVar(&opt.Methods, "method", nil, "Allowed method: GET/PUT/POST/DELETE/HEAD (repeatable)")
	cmd.Flags().StringArrayVar(&opt.AllowedHeaders, "allowed-header", nil, "Allowed request header, e.g. Authorization (repeatable)")
	cmd.Flags().StringArrayVar(&opt.ExposeHeaders, "expose-header", nil, "Exposed response header (repeatable)")
	cmd.Flags().IntVar(&opt.MaxAgeSeconds, "max-age", 0, "Max age in seconds for preflight responses")
	cmd.Flags().StringVarP(&opt.ConfigFile, "from-file", "", "", "Load CORS rules from a JSON/XML file (overrides flags)")
	return cmd
}

func CorsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "get [alias:bucket] ...",
		Short:             "Print CORS rules of bucket(s) as JSON",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.GetCors(dst.Bucket)
		}),
	}
}

// CorsRemoveCmd 删除桶 CORS (mc 子命令名 remove, 兼容旧名 del)
func CorsRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "remove [alias:bucket] ...",
		Aliases:           []string{"del"},
		Short:             "Delete CORS rules for bucket(s)",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.DelCors(dst.Bucket)
		}),
	}
}

// PolicyCmd 管理桶的访问策略 (permission 语义对齐 mc anonymous).
func PolicyCmd() *cobra.Command {
	policyCmd := &cobra.Command{
		Use:   "policy",
		Short: "Manage bucket policy (mc anonymous compatible)",
	}
	policyCmd.AddCommand(
		PolicySetCmd(),
		PolicyGetCmd(),
		PolicyDelCmd(),
	)
	return policyCmd
}

// knownPermissions mc anonymous 的权限取值 (含兼容旧名).
var knownPermissions = map[string]bool{
	"private": true, "download": true, "upload": true, "public": true,
	"none": true, "public-read": true, "public-write": true, "public-read-write": true,
}

// PolicySetCmd 设置桶策略: mc 风格 `set PERMISSION [alias:bucket]`,
// 或 flag 方式 `set [alias:bucket] --type PERMISSION`; 自定义 JSON 用 -f.
func PolicySetCmd() *cobra.Command {
	var opt action.PolicyOptions
	cmd := &cobra.Command{
		Use:               "set [permission] [alias:bucket] ...",
		Short:             "Set bucket policy: private/download/upload/public (mc anonymous set compatible)",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunEWithPreprocess(
			func(_ *cobra.Command, args []string) ([]string, error) {
				var bucketArgs []string
				if knownPermissions[args[0]] {
					opt.Permission = args[0]
					bucketArgs = args[1:]
				} else {
					bucketArgs = args
				}
				if len(bucketArgs) == 0 {
					return nil, fmt.Errorf("policy set: no target bucket given (usage: policy set PERMISSION [alias:bucket] ...)")
				}
				return bucketArgs, nil
			},
			func(S3 action.Action, sp *s3path.Path) error {
				return S3.SetPolicy(opt, sp.Bucket)
			},
		),
	}
	cmd.Flags().StringVar(&opt.Permission, "type", "", "Permission: private/download/upload/public (legacy: --type)")
	cmd.Flags().StringVar(&opt.Prefix, "prefix", "", "Scope the permission to objects under this key prefix (default: whole bucket)")
	cmd.Flags().StringVarP(&opt.ConfigFile, "from-file", "", "", "Set policy from a custom JSON file (overrides permission)")
	return cmd
}

func PolicyGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "get [alias:bucket] ...",
		Short:             "Print current bucket policy (pretty JSON)",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.GetPolicy(dst.Bucket)
		}),
	}
}

func PolicyDelCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "del [alias:bucket] ...",
		Short:             "Delete bucket policy",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.DelPolicy(dst.Bucket)
		}),
	}
}

// isLocalFile 判断路径是否为本地已存在的普通文件.
func isLocalFile(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
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
		Annotations:       ParseArgsAndS3Path,
		RunE: NewRunEWithMode(func(S3 action.Action, dst *s3path.Path, opts ArgParseMode) error {
			return S3.SetNotification(opts[AddedArgs], dst.Bucket)
		}),
	}
}

func EventGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "get [alias:bucket] ...",
		Short:             "Print bucket(s) event notification configuration (JSON)",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.GetNotification(dst.Bucket)
		}),
	}
}

func EventDelCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "del [alias:bucket] ...",
		Short:             "Remove all bucket event notification configurations",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.DelNotification(dst.Bucket)
		}),
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
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.SetEncryption(encOpt, dst.Bucket)
		}),
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
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.GetEncryption(dst.Bucket)
		}),
	}
}

func EncryptionDelCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "del [alias:bucket] ...",
		Short:             "Delete bucket(s) default encryption configuration",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.DelEncryption(dst.Bucket)
		}),
	}
}

// VersioningCmd 管理桶的版本控制配置 (mc version 对齐: enable/suspend/info; 兼容 set).
func VersioningCmd() *cobra.Command {
	versionCmd := &cobra.Command{
		Use:   "versioning",
		Short: "Manage bucket versioning",
	}

	versionCmd.AddCommand(VersioningEnableCmd(), VersioningSuspendCmd(), VersioningInfoCmd(), VersioningSetCmd())
	return versionCmd
}

// VersioningEnableCmd 启用版本控制 (mc version enable).
func VersioningEnableCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "enable [alias:bucket] ...",
		Short:             "Enable bucket versioning",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.SetVersioning(dst.Bucket, "Enabled")
		}),
	}
}

// VersioningSuspendCmd 暂停版本控制 (mc version suspend).
func VersioningSuspendCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "suspend [alias:bucket] ...",
		Short:             "Suspend bucket versioning",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.SetVersioning(dst.Bucket, "Suspended")
		}),
	}
}

// VersioningSetCmd 兼容旧入口: --status 指定状态.
func VersioningSetCmd() *cobra.Command {
	var set string
	cmd := &cobra.Command{
		Use:               "set [alias:bucket] ...",
		Short:             "Set bucket versioning status (legacy; use enable/suspend)",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.SetVersioning(dst.Bucket, set)
		}),
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
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.GetVersioning(dst.Bucket)
		}),
	}
}
