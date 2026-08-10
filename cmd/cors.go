package cmd

import (
	"s3cli/internal/action"
	"s3cli/internal/s3path"

	"github.com/spf13/cobra"
)

// CorsCmd 管理桶的 CORS 配置
func CorsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cors",
		Short: "Manage CORS configuration for bucket",
	}
	cmd.AddCommand(CorsSetCmd(), CorsGetCmd(), CorsRemoveCmd())
	return cmd
}

// CorsSetCmd 设置桶 CORS
func CorsSetCmd() *cobra.Command {
	var opt action.CorsOptions
	cmd := &cobra.Command{
		Use:               "set [alias:bucket] ...",
		Short:             "Set CORS rules for bucket (--origin/--method flags or JSON/XML file)",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.SetCors(opt, dst.Bucket)
		}),
	}
	cmd.Flags().StringVar(&opt.ID, "id", "", "CORS rule ID")
	cmd.Flags().StringArrayVar(&opt.Origins, "origin", nil, "Allowed origin, e.g. https://example.com or * (repeatable)")
	cmd.Flags().StringArrayVar(&opt.Methods, "method", nil, "Allowed method: GET/PUT/POST/DELETE/HEAD (repeatable)")
	cmd.Flags().StringArrayVar(&opt.AllowedHeaders, "allowed-header", nil, "Allowed request header, e.g. Authorization (repeatable)")
	cmd.Flags().StringArrayVar(&opt.ExposeHeaders, "expose-header", nil, "Exposed response header (repeatable)")
	cmd.Flags().IntVar(&opt.MaxAgeSeconds, "max-age", 0, "Max age in seconds for preflight responses")
	cmd.Flags().StringVar(&opt.ConfigFile, "from-file", "", "Load CORS rules from a JSON/XML file (overrides flags)")
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
