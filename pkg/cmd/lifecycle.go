package cmd

import (
	"s3cli/pkg/action"
	"s3cli/pkg/utils"

	"github.com/spf13/cobra"
)

func init() { Register("config", "Bucket Configuration", NewLifecycleCmd) }

func NewLifecycleCmd() *cobra.Command {
	lifecyCmd := &cobra.Command{
		Use:   "lifecycle",
		Short: "Manage lifecycle rules",
	}
	lifecyCmd.AddCommand(
		NewSetLifecycleCmd(),
		NewGetLifecycleCmd(),
		NewDelLifecycleCmd(),
	)
	return lifecyCmd
}

func NewSetLifecycleCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set [lifecycle-file] [alias:bucket] ...",
		Short: "Set lifecycle rules (JSON, AWS CLI compatible)",
		Args:  cobra.MinimumNArgs(2),
		RunE: NewRunE(ActionFunc(func(S3 action.S3Client, opts *CmdContext, s3path *utils.S3Path) error {
			return S3.SetLifecycle(opts.Global.LocalFile, s3path.Bucket)
		}), &CmdContext{ArgParseMode: ParseLocalFileAndS3Path}),
	}
}

func NewGetLifecycleCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "get [alias:bucket] ...",
		Aliases: []string{"ls", "list"},
		Short:   "Print current lifecycle rules (JSON)",
		Args:    cobra.MinimumNArgs(1),
		RunE: NewRunE(ActionFunc(func(S3 action.S3Client, _ *CmdContext, s3path *utils.S3Path) error {
			return S3.GetLifecycle(s3path.Bucket)
		}), nil),
	}
}

func NewDelLifecycleCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "del [s3://bucket] ...",
		Aliases: []string{"delete", "remove"},
		Short:   "Delete all lifecycle rules",
		Args:    cobra.MinimumNArgs(1),
		RunE: NewRunE(ActionFunc(func(S3 action.S3Client, _ *CmdContext, s3path *utils.S3Path) error {
			return S3.DelLifecycle(s3path.Bucket)
		}), nil),
	}
}
