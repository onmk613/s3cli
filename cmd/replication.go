package cmd

import (
	"s3cli/internal/action"
	"s3cli/internal/i18n"
	"s3cli/internal/s3path"

	"github.com/spf13/cobra"
)

// ReplicationCmd 管理桶的复制配置 (get/set/remove), 挂载在 `s3cli bucket` 下。
func ReplicationCmd() *cobra.Command {
	replicationCmd := &cobra.Command{
		Use:   "replication",
		Short: i18n.T("Manage bucket replication configuration", "管理存储桶复制配置"),
	}
	replicationCmd.AddCommand(ReplicationGetCmd(), ReplicationSetCmd(), ReplicationRemoveCmd())
	return replicationCmd
}

// ReplicationGetCmd 打印桶的复制配置 (JSON)。
func ReplicationGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "get [alias:bucket] ...",
		Short:             i18n.T("Print bucket replication configuration (JSON)", "打印存储桶复制配置（JSON）"),
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.GetReplication(dst.Bucket)
		}),
	}
}

// ReplicationSetCmd 从 JSON 文件设置桶的复制配置。
func ReplicationSetCmd() *cobra.Command {
	var opt action.ReplicationOptions
	cmd := &cobra.Command{
		Use:               "set [alias:bucket] ...",
		Short:             i18n.T("Set bucket replication configuration from a JSON file (--from-file)", "从 JSON 文件设置存储桶复制配置（--from-file）"),
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.SetReplication(opt, dst.Bucket)
		}),
	}
	cmd.Flags().StringVar(&opt.ConfigFile, "from-file", "", i18n.T("Load AWS CLI JSON config (Role / Rules)", "从 AWS CLI JSON 配置加载（Role / Rules）"))
	_ = cmd.MarkFlagRequired("from-file")
	return cmd
}

// ReplicationRemoveCmd 删除桶的复制配置。
func ReplicationRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "remove [alias:bucket] ...",
		Aliases:           []string{"rm", "del", "delete"},
		Short:             i18n.T("Delete bucket replication configuration", "删除存储桶复制配置"),
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.DelReplication(dst.Bucket)
		}),
	}
}
