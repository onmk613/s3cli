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
	var opts action.RemoveBucketOptions
	cmd := &cobra.Command{
		Use:               "remove [alias:bucket] ...",
		Short:             "Remove bucket",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: AutoCompleteBucket,
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.RemoveBuckets(opts, dst.Bucket)
		}),
	}
	cmd.Flags().BoolVar(&opts.Force, "force", false, "Force remove bucket even if not empty")
	return cmd
}
