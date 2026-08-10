package cmd

import (
	"s3cli/internal/action"
	"s3cli/internal/s3path"

	"github.com/spf13/cobra"
)

// PolicyCmd 管理桶的访问策略 (permission 语义对齐 mc anonymous).
func PolicyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policy",
		Short: "Manage bucket policy (mc anonymous compatible)",
	}
	cmd.AddCommand(PolicySetCmd(), PolicyGetCmd(), PolicyDelCmd())
	return cmd
}

// PolicySetCmd 设置桶策略
func PolicySetCmd() *cobra.Command {
	var opt action.PolicyOptions
	cmd := &cobra.Command{
		Use:               "set [alias:bucket] ...",
		Short:             "Set bucket policy: private/download/upload/public (mc anonymous set compatible)",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.SetPolicy(opt, dst.Bucket)
		}),
	}
	cmd.Flags().StringVar(&opt.Permission, "type", "", "Permission: private/download/upload/public (legacy: --type)")
	cmd.Flags().StringVar(&opt.Prefix, "prefix", "", "Scope the permission to objects under this key prefix (default: whole bucket)")
	cmd.Flags().StringVar(&opt.ConfigFile, "from-file", "", "Set policy from a custom JSON file (overrides permission)")
	return cmd
}

func PolicyGetCmd() *cobra.Command {
	var opt action.GetPolicyOptions
	cmd := &cobra.Command{
		Use:               "get [alias:bucket] ...",
		Short:             "Print current bucket policy (pretty JSON)",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.GetPolicy(opt, dst.Bucket)
		}),
	}
	cmd.Flags().BoolVar(&opt.JSON, "json", false, "Print the original policy JSON instead of the type")
	return cmd
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
