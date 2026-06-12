package cmd

import (
	"s3cli/pkg/action"
	"s3cli/pkg/utils"

	"github.com/spf13/cobra"
)

func init() { Register("config", "Bucket Configuration", NewVersionCmd) }

func NewVersionCmd() *cobra.Command {
	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Manage bucket versioning",
	}

	versionCmd.AddCommand(NewVersionEnableCmd(), NewVersionSuspendCmd(), NewVersionInfoCmd())
	return versionCmd
}

func NewVersionEnableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enabled [alias:bucket] ...",
		Short: "Enable bucket versioning",
		Args:  cobra.MinimumNArgs(1),
		RunE: NewRunE(ActionFunc(func(S3 action.S3Client, _ *CmdContext, s3path *utils.S3Path) error {
			return S3.SetVersioning(s3path.Bucket, true)
		}), nil),
	}
}

func NewVersionSuspendCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "suspended [alias:bucket] ...",
		Short: "Suspend bucket versioning",
		Args:  cobra.MinimumNArgs(1),
		RunE: NewRunE(ActionFunc(func(S3 action.S3Client, _ *CmdContext, s3path *utils.S3Path) error {
			return S3.SetVersioning(s3path.Bucket, false)
		}), nil),
	}
}

func NewVersionInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "info [alias:bucket] ...",
		Aliases: []string{"list", "ls", "get"},
		Short:   "Print current versioning status",
		Args:    cobra.MinimumNArgs(1),
		RunE: NewRunE(ActionFunc(func(S3 action.S3Client, _ *CmdContext, s3path *utils.S3Path) error {
			return S3.GetVersioning(s3path.Bucket)
		}), nil),
	}
}
