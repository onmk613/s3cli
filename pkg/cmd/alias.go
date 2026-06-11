package cmd

import (
	"s3cli/pkg/config"

	"github.com/spf13/cobra"
)

func init() {
	Register("alias", "Server Management", NewAliasCmd)
}

func NewAliasCmd() *cobra.Command {
	aliasCmd := &cobra.Command{
		Use:     "alias",
		Aliases: []string{"a"},
		Short:   "Manage aliases (S3 endpoint configurations)",
	}
	aliasCmd.AddCommand(setAliasCmd(), listAliasCmd(), delAliasCmd())
	return aliasCmd
}

func setAliasCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "set [alias]",
		Aliases: []string{"s"},
		Short:   "Set alias, alias_name must be unique, e.g. alias set mys3Server",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return config.SetAliasConf(cmd.Context(), args[0])
		},
	}
}

func listAliasCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls", "l"},
		Short:   "List aliases",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return config.ListAliasConf()
		},
	}
}

func delAliasCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "del [alias]",
		Aliases: []string{"delete", "rm", "remove", "d"},
		Short:   "Delete an alias",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return config.DelConf(args)
		},
	}
}
