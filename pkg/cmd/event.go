package cmd

import (
	"s3cli/pkg/action"
	"s3cli/pkg/utils"

	"github.com/spf13/cobra"
)

func init() { Register("config", "Bucket Configuration", NewEventCmd) }

func NewEventCmd() *cobra.Command {
	eventCmd := &cobra.Command{
		Use:     "event",
		Aliases: []string{"notification"},
		Short:   "Manage object notifications",
	}
	eventCmd.AddCommand(NewSetEventCmd(), NewGetEventCmd(), NewDelEventCmd())
	return eventCmd
}

func NewSetEventCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set [notification-file] [alias:bucket] ...",
		Short: "Set bucket event notifications (SQS/SNS/Lambda, JSON, AWS CLI compatible)",
		Args:  cobra.MinimumNArgs(2),
		RunE: NewRunE(ActionFunc(func(S3 action.S3Client, opts *CmdContext, s3path *utils.S3Path) error {
			return S3.SetNotification(opts.Global.LocalFile, s3path.Bucket)
		}), &CmdContext{ArgParseMode: ParseLocalFileAndS3Path}),
	}
}

func NewGetEventCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "get [alias:bucket] ...",
		Aliases: []string{"ls", "list"},
		Short:   "Print bucket event notification configuration (JSON)",
		Args:    cobra.MinimumNArgs(1),
		RunE: NewRunE(ActionFunc(func(S3 action.S3Client, _ *CmdContext, s3path *utils.S3Path) error {
			return S3.GetNotification(s3path.Bucket)
		}), nil),
	}
}

func NewDelEventCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "del [alias:bucket] ...",
		Aliases: []string{"delete", "remove"},
		Short:   "Remove all bucket event notification configurations",
		Args:    cobra.MinimumNArgs(1),
		RunE: NewRunE(ActionFunc(func(S3 action.S3Client, _ *CmdContext, s3path *utils.S3Path) error {
			return S3.DelNotification(s3path.Bucket)
		}), nil),
	}
}
