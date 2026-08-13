package cmd

import (
	"s3cli/internal/config"
	"s3cli/pkg/i18n"

	"github.com/spf13/cobra"
)

//
// Alias 别名管理：
//	 s3cli alias set myS3Server               // 交互式配置 myS3Server 的 endpoint、access_key、secret_key 等信息
//	 s3cli alias set myS3Server URL AK SK     // 非交互写入/覆盖
//   s3cli alias edit myS3Server              // 交互式修改已有 alias 的配置
//   s3cli alias list (可指定 alias 名称，列出指定 alias 的配置)
//	 s3cli alias del myS3Server
// 注意：
//   alias 名称必须唯一，否者会覆盖
//   list、edit 和 del 支持 tab 补全
//

func init() {
	Register("alias", "Endpoint Management", NewAliasCmd)
}

func NewAliasCmd() *cobra.Command {
	aliasCmd := &cobra.Command{
		Use:     "alias",
		Aliases: []string{"a", "server"},
		Short:   i18n.T("Manage aliases (S3 endpoint configurations)", "管理别名（S3 endpoint 配置）"),
	}
	aliasCmd.AddCommand(setAliasCmd(), editAliasCmd(), listAliasCmd(), delAliasCmd())
	return aliasCmd
}

func setAliasCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "add [ALIAS] [URL] [ACCESSKEY] [SECRETKEY] [SESSIONTOKEN]",
		Aliases:           []string{"set", "s", "create"},
		Short:             i18n.T("Add an alias: interactive with 1 arg, or ALIAS URL ACCESSKEY SECRETKEY [SESSIONTOKEN]", "添加别名：带 1 个参数进入交互式配置，或直接指定 ALIAS URL ACCESSKEY SECRETKEY [SESSIONTOKEN]"),
		ValidArgsFunction: AutoCompleteAlias,
		Args:              cobra.RangeArgs(1, 5),
		RunE: func(cmd *cobra.Command, args []string) error {
			return config.SetAliasConf(args)
		},
	}
}

func editAliasCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "edit ALIAS",
		Aliases:           []string{"e", "modify", "update"},
		Short:             i18n.T("Edit an alias interactively (empty input keeps the current value)", "交互式编辑别名（输入为空则保留原值）"),
		ValidArgsFunction: AutoCompleteAlias,
		Args:              cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return config.EditAliasConf(cmd.Context(), args[0])
		},
	}
}

func listAliasCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "list",
		Aliases:           []string{"ls", "l", "show", "get"},
		Short:             i18n.T("List aliases", "列出别名"),
		ValidArgsFunction: AutoCompleteAlias,
		Args:              cobra.MinimumNArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			return config.ListAliasConf(args)
		},
	}
	cmd.Flags().BoolVarP(&config.G.F.ShowSecret, "show-secret", "s", false, i18n.T("Reveal full secret keys", "显示完整的 secret key"))
	return cmd
}

func delAliasCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "del [alias]",
		Aliases:           []string{"delete", "rm", "remove", "d"},
		Short:             i18n.T("Delete an alias", "删除别名"),
		ValidArgsFunction: AutoCompleteAlias,
		Args:              cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return config.DelConf(args)
		},
	}
}
