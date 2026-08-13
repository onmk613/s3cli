package cmd

import (
	"s3cli/internal/action"
	"s3cli/internal/s3path"
	"s3cli/pkg/i18n"

	"github.com/spf13/cobra"
)

// VersioningCmd 管理桶的版本控制配置 (enable/suspend/info; 兼容 set).
func VersioningCmd() *cobra.Command {
	versionCmd := &cobra.Command{
		Use:   "versioning",
		Short: i18n.T("Manage bucket versioning", "管理存储桶版本控制"),
	}

	versionCmd.AddCommand(VersioningInfoCmd(), VersioningSetCmd())
	return versionCmd
}

// VersioningSetCmd 兼容旧入口: --status 指定状态.
func VersioningSetCmd() *cobra.Command {
	var opts action.VersioningOptions
	cmd := &cobra.Command{
		Use:               "set [alias:bucket] ...",
		Short:             i18n.T("Set bucket versioning status (legacy; use enable/suspend)", "设置存储桶版本控制状态（旧入口；建议用 enable/suspend）"),
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.SetVersioning(opts, dst.Bucket)
		}),
	}

	// --status 选项必须指定
	// 不同服务端在关闭版本控制上的行为不同
	// 有的使用 Suspended, 有的使用 Disabled
	cmd.Flags().StringVar(&opts.Status, "status", "", i18n.T("Set bucket versioning status (Enabled/Suspended/Disabled)", "设置存储桶版本控制状态（Enabled/Suspended/Disabled）"))
	_ = cmd.MarkFlagRequired("status")
	return cmd
}

func VersioningInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "info [alias:bucket] ...",
		Short:             i18n.T("Print current versioning status", "打印当前版本控制状态"),
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.GetVersioning(dst.Bucket)
		}),
	}
}
