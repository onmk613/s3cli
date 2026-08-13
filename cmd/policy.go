package cmd

import (
	"s3cli/internal/action"
	"s3cli/internal/s3path"
	"s3cli/pkg/i18n"

	"github.com/spf13/cobra"
)

// PolicyCmd 管理桶的访问策略 (permission 语义: private/download/upload/public).
func PolicyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policy",
		Short: i18n.T("Manage bucket policy (private/download/upload/public)", "管理存储桶访问策略（private/download/upload/public）"),
	}
	cmd.AddCommand(PolicySetCmd(), PolicyGetCmd(), PolicyDelCmd())
	return cmd
}

// PolicySetCmd 设置桶策略
func PolicySetCmd() *cobra.Command {
	var opt action.PolicyOptions
	cmd := &cobra.Command{
		Use:               "set [alias:bucket] ...",
		Short:             i18n.T("Set bucket policy: private/download/upload/public", "设置存储桶策略：private/download/upload/public"),
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.SetPolicy(opt, dst.Bucket)
		}),
	}
	cmd.Flags().StringVar(&opt.Permission, "type", "", i18n.T("Permission: private/download/upload/public (legacy: --type)", "权限：private/download/upload/public（旧写法：--type）"))
	cmd.Flags().StringVar(&opt.Prefix, "prefix", "", i18n.T("Scope the permission to objects under this key prefix (default: whole bucket)", "把权限限定到该 key 前缀下的对象（默认：整个存储桶）"))
	cmd.Flags().StringVar(&opt.ConfigFile, "from-file", "", i18n.T("Set policy from a custom JSON file (overrides permission)", "从自定义 JSON 文件设置策略（覆盖权限设置）"))
	return cmd
}

func PolicyGetCmd() *cobra.Command {
	var opt action.GetPolicyOptions
	cmd := &cobra.Command{
		Use:               "get [alias:bucket] ...",
		Short:             i18n.T("Print current bucket policy (pretty JSON)", "打印当前存储桶策略（美化 JSON）"),
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.GetPolicy(opt, dst.Bucket)
		}),
	}
	cmd.Flags().BoolVar(&opt.JSON, "json", false, i18n.T("Print the original policy JSON instead of the type", "打印原始策略 JSON，而非策略类型"))
	return cmd
}

func PolicyDelCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "del [alias:bucket] ...",
		Short:             i18n.T("Delete bucket policy", "删除存储桶策略"),
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.DelPolicy(dst.Bucket)
		}),
	}
}
