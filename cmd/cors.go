package cmd

import (
	"s3cli/internal/action"
	"s3cli/internal/s3path"
	"s3cli/pkg/i18n"

	"github.com/spf13/cobra"
)

// CorsCmd 管理桶的 CORS 配置
func CorsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cors",
		Short: i18n.T("Manage CORS configuration for bucket", "管理存储桶的 CORS 配置"),
	}
	cmd.AddCommand(CorsSetCmd(), CorsGetCmd(), CorsRemoveCmd())
	return cmd
}

// CorsSetCmd 设置桶 CORS
func CorsSetCmd() *cobra.Command {
	var opt action.CorsOptions
	cmd := &cobra.Command{
		Use:               "set [alias:bucket] ...",
		Short:             i18n.T("Set CORS rules for bucket (--origin/--method flags or JSON/XML file)", "设置存储桶 CORS 规则（--origin/--method 标志或 JSON/XML 文件）"),
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.SetCors(opt, dst.Bucket)
		}),
	}
	cmd.Flags().StringVar(&opt.ID, "id", "", i18n.T("CORS rule ID", "CORS 规则 ID"))
	cmd.Flags().StringArrayVar(&opt.Origins, "origin", nil, i18n.T("Allowed origin, e.g. https://example.com or * (repeatable)", "允许的来源，如 https://example.com 或 *（可重复）"))
	cmd.Flags().StringArrayVar(&opt.Methods, "method", nil, i18n.T("Allowed method: GET/PUT/POST/DELETE/HEAD (repeatable)", "允许的方法：GET/PUT/POST/DELETE/HEAD（可重复）"))
	cmd.Flags().StringArrayVar(&opt.AllowedHeaders, "allowed-header", nil, i18n.T("Allowed request header, e.g. Authorization (repeatable)", "允许的请求头，如 Authorization（可重复）"))
	cmd.Flags().StringArrayVar(&opt.ExposeHeaders, "expose-header", nil, i18n.T("Exposed response header (repeatable)", "暴露的响应头（可重复）"))
	cmd.Flags().IntVar(&opt.MaxAgeSeconds, "max-age", 0, i18n.T("Max age in seconds for preflight responses", "预检请求响应的最大缓存时间（秒）"))
	cmd.Flags().StringVar(&opt.ConfigFile, "from-file", "", i18n.T("Load CORS rules from a JSON/XML file (overrides flags)", "从 JSON/XML 文件加载 CORS 规则（覆盖标志设置）"))
	return cmd
}

func CorsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "get [alias:bucket] ...",
		Short:             i18n.T("Print CORS rules of bucket(s) as JSON", "以 JSON 打印存储桶的 CORS 规则"),
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.GetCors(dst.Bucket)
		}),
	}
}

// CorsRemoveCmd 删除桶 CORS (兼容旧名 del)
func CorsRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "remove [alias:bucket] ...",
		Aliases:           []string{"del"},
		Short:             i18n.T("Delete CORS rules for bucket(s)", "删除存储桶的 CORS 规则"),
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.DelCors(dst.Bucket)
		}),
	}
}
