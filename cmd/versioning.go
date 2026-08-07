package cmd

import (
	"s3cli/internal/action"
	"s3cli/internal/s3path"

	"github.com/spf13/cobra"
)

// VersioningCmd 管理桶的版本控制配置 (mc version 对齐: enable/suspend/info; 兼容 set).
func VersioningCmd() *cobra.Command {
	versionCmd := &cobra.Command{
		Use:   "versioning",
		Short: "Manage bucket versioning",
	}

	versionCmd.AddCommand(VersioningInfoCmd(), VersioningSetCmd())
	return versionCmd
}

// VersioningSetCmd 兼容旧入口: --status 指定状态.
func VersioningSetCmd() *cobra.Command {
	var opts action.VersioningOptions
	cmd := &cobra.Command{
		Use:               "set [alias:bucket] ...",
		Short:             "Set bucket versioning status (legacy; use enable/suspend)",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.SetVersioning(opts, dst.Bucket)
		}),
	}

	// --status 选项必须指定
	// 兼容 MinIO 和 AWS 在关闭版本控制上不同的行为
	// MinIO 使用Suspended，AWS使用Disabled
	cmd.Flags().StringVar(&opts.Status, "status", "", "Set bucket versioning status (Enabled/Suspended/Disabled)")
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
