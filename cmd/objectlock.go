package cmd

import (
	"s3cli/internal/action"
	"s3cli/internal/i18n"
	"s3cli/internal/s3path"

	"github.com/spf13/cobra"
)

// ObjectLockCmd 管理桶的 Object Lock 默认保留配置 (get/set), 挂载在 `s3cli bucket` 下。
func ObjectLockCmd() *cobra.Command {
	lockCmd := &cobra.Command{
		Use:   "object-lock",
		Short: i18n.T("Manage bucket Object Lock configuration (default retention)", "管理存储桶对象锁配置（默认保留策略）"),
	}
	lockCmd.AddCommand(ObjectLockGetCmd(), ObjectLockSetCmd())
	return lockCmd
}

// ObjectLockGetCmd 打印桶的 Object Lock 配置 (JSON)。
func ObjectLockGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "get [alias:bucket] ...",
		Short:             i18n.T("Print bucket Object Lock configuration (JSON)", "打印存储桶对象锁配置（JSON）"),
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.GetObjectLockConfiguration(dst.Bucket)
		}),
	}
}

// ObjectLockSetCmd 从 JSON 文件设置桶的 Object Lock 配置。
func ObjectLockSetCmd() *cobra.Command {
	var opt action.ObjectLockConfigOptions
	cmd := &cobra.Command{
		Use:               "set [alias:bucket] ...",
		Short:             i18n.T("Set bucket Object Lock configuration from a JSON file (--from-file)", "从 JSON 文件设置存储桶对象锁配置（--from-file）"),
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.SetObjectLockConfiguration(opt, dst.Bucket)
		}),
	}
	cmd.Flags().StringVar(&opt.ConfigFile, "from-file", "", i18n.T("Load AWS CLI JSON config (ObjectLockEnabled / Rule)", "从 AWS CLI JSON 配置加载（ObjectLockEnabled / Rule）"))
	_ = cmd.MarkFlagRequired("from-file")
	return cmd
}

// ObjectRetentionCmd 管理对象保留设置 (get/set)。
func ObjectRetentionCmd() *cobra.Command {
	retentionCmd := &cobra.Command{
		Use:   "retention",
		Short: i18n.T("Manage object retention (Object Lock)", "管理对象保留策略（对象锁）"),
	}
	retentionCmd.AddCommand(ObjectRetentionGetCmd(), ObjectRetentionSetCmd())
	return retentionCmd
}

// ObjectRetentionGetCmd 打印对象的保留设置。
func ObjectRetentionGetCmd() *cobra.Command {
	var versionID string
	cmd := &cobra.Command{
		Use:               "get [alias:bucket/key] ...",
		Short:             i18n.T("Print object retention (JSON)", "打印对象保留策略（JSON）"),
		ValidArgsFunction: AutoCompletePath,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.GetObjectRetention(dst.Bucket, dst.Key, versionID)
		}),
	}
	addVersionIDFlags(cmd, &versionID, i18n.T("Read the retention of a specific object version", "查看对象特定版本的保留策略"))
	return cmd
}

// ObjectRetentionSetCmd 设置对象的保留模式与到期时间。
func ObjectRetentionSetCmd() *cobra.Command {
	var opt action.RetentionOptions
	var versionID string
	cmd := &cobra.Command{
		Use:               "set [alias:bucket/key] ...",
		Short:             i18n.T("Set object retention (--mode GOVERNANCE|COMPLIANCE --retain-until RFC3339)", "设置对象保留策略（--mode GOVERNANCE|COMPLIANCE --retain-until RFC3339）"),
		ValidArgsFunction: AutoCompletePath,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.SetObjectRetention(opt, dst.Bucket, dst.Key, versionID)
		}),
	}
	addVersionIDFlags(cmd, &versionID, i18n.T("Set the retention of a specific object version", "设置对象特定版本的保留策略"))
	f := cmd.Flags()
	f.StringVar(&opt.Mode, "mode", "", i18n.T("Retention mode: GOVERNANCE or COMPLIANCE", "保留模式：GOVERNANCE 或 COMPLIANCE"))
	f.StringVar(&opt.RetainUntil, "retain-until", "", i18n.T("Retention expiration time in RFC3339, e.g. 2030-01-02T15:04:05Z", "保留到期时间（RFC3339），如 2030-01-02T15:04:05Z"))
	_ = cmd.MarkFlagRequired("mode")
	return cmd
}

// ObjectLegalHoldCmd 管理对象法律留存 (get/set)。
func ObjectLegalHoldCmd() *cobra.Command {
	holdCmd := &cobra.Command{
		Use:     "legal-hold",
		Aliases: []string{"legalhold"},
		Short:   i18n.T("Manage object legal hold (Object Lock)", "管理对象法律留存（对象锁）"),
	}
	holdCmd.AddCommand(ObjectLegalHoldGetCmd(), ObjectLegalHoldSetCmd())
	return holdCmd
}

// ObjectLegalHoldGetCmd 打印对象的法律留存状态。
func ObjectLegalHoldGetCmd() *cobra.Command {
	var versionID string
	cmd := &cobra.Command{
		Use:               "get [alias:bucket/key] ...",
		Short:             i18n.T("Print object legal hold status (JSON)", "打印对象法律留存状态（JSON）"),
		ValidArgsFunction: AutoCompletePath,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.GetObjectLegalHold(dst.Bucket, dst.Key, versionID)
		}),
	}
	addVersionIDFlags(cmd, &versionID, i18n.T("Read the legal hold of a specific object version", "查看对象特定版本的法律留存"))
	return cmd
}

// ObjectLegalHoldSetCmd 设置对象的法律留存状态 (ON / OFF)。
func ObjectLegalHoldSetCmd() *cobra.Command {
	var opt action.LegalHoldOptions
	var versionID string
	cmd := &cobra.Command{
		Use:               "set [alias:bucket/key] ...",
		Short:             i18n.T("Set object legal hold status (--status ON|OFF)", "设置对象法律留存状态（--status ON|OFF）"),
		ValidArgsFunction: AutoCompletePath,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.SetObjectLegalHold(opt, dst.Bucket, dst.Key, versionID)
		}),
	}
	addVersionIDFlags(cmd, &versionID, i18n.T("Set the legal hold of a specific object version", "设置对象特定版本的法律留存"))
	cmd.Flags().StringVar(&opt.Status, "status", "", i18n.T("Legal hold status: ON or OFF", "法律留存状态：ON 或 OFF"))
	_ = cmd.MarkFlagRequired("status")
	return cmd
}

// addVersionIDFlags 注册 --version-id/--vid (两处别名指向同一变量, 与 stat 等命令一致)。
func addVersionIDFlags(cmd *cobra.Command, versionID *string, desc string) {
	cmd.Flags().StringVar(versionID, "version-id", "", desc)
	cmd.Flags().StringVar(versionID, "vid", "", vidAliasDesc())
}
