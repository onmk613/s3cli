package cmd

import (
	"s3cli/internal/config"

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
		Short:   "Manage aliases (S3 endpoint configurations)",
	}
	aliasCmd.AddCommand(setAliasCmd(), editAliasCmd(), listAliasCmd(), delAliasCmd())
	return aliasCmd
}

func setAliasCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "add [ALIAS] [URL] [ACCESSKEY] [SECRETKEY] [SESSIONTOKEN]",
		Aliases:           []string{"set", "s", "create"},
		Short:             "Add an alias: interactive with 1 arg, or ALIAS URL ACCESSKEY SECRETKEY [SESSIONTOKEN]",
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
		Short:             "Edit an alias interactively (empty input keeps the current value)",
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
		Short:             "List aliases",
		ValidArgsFunction: AutoCompleteAlias,
		Args:              cobra.MinimumNArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			return config.ListAliasConf(args)
		},
	}
	cmd.Flags().BoolVarP(&config.G.F.ShowSecret, "show-secret", "s", false, "Reveal full secret keys")
	return cmd
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
