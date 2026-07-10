package cmd

import (
	"s3cli/pkg/config"

	"github.com/spf13/cobra"
)

//
// Alias 别名管理：
//	 s3cli alias set myS3Server // 会进入交互式配置界面，设置 myS3Server 的 endpoint、access_key、secret_key 等信息
//   s3cli alias list (可指定 alias 名称，列出指定 alias 的配置)
//	 s3cli alias del myS3Server
// 注意：
//   alias 名称必须唯一，否者会覆盖
//   list 和 del 支持tab 补全
//

func init() {
	Register("alias", "Endpoint Management", NewAliasCmd)
}

func NewAliasCmd() *cobra.Command {
	aliasCmd := &cobra.Command{
		Use:     "alias",
		Aliases: []string{"a", "server"},
		Short:   "Manage aliases (S3 endpoint configurations)",
	}
	aliasCmd.AddCommand(setAliasCmd(), listAliasCmd(), delAliasCmd())
	return aliasCmd
}

func setAliasCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "set [alias]",
		Aliases: []string{"s", "add", "create"},
		Short:   "Set alias, alias_name must be unique, e.g. alias set mys3Server",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return config.SetAliasConf(cmd.Context(), args[0])
		},
	}
}

func listAliasCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "list",
		Aliases:           []string{"ls", "l", "show", "get"},
		Short:             "List aliases",
		ValidArgsFunction: AutoCompleteAlias,
		Args:              cobra.MinimumNArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			return config.ListAliasConf(args)
		},
	}
}

func delAliasCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "del [alias]",
		Aliases:           []string{"delete", "rm", "remove", "d"},
		Short:             "Delete an alias",
		ValidArgsFunction: AutoCompleteAlias,
		Args:              cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return config.DelConf(args)
		},
	}
}
