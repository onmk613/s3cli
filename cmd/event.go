package cmd

import (
	"s3cli/internal/action"
	"s3cli/internal/s3path"

	"github.com/spf13/cobra"
)

// EventCmd 管理桶的事件通知配置
func EventCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "event",
		Short: "Manage object notifications",
	}
	cmd.AddCommand(EventSetCmd(), EventGetCmd(), EventDelCmd())
	return cmd
}

func EventSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "set [notification-file] [alias:bucket] ...",
		Short:             "Set bucket event notifications (SQS/SNS/Lambda, JSON, AWS CLI compatible)",
		ValidArgsFunction: CompleteLocalFirst(AutoCompleteBucket),
		Args:              cobra.MinimumNArgs(2),
		Annotations:       ParseArgsAndS3Path,
		RunE: NewRunEWithMode(func(S3 action.Action, dst *s3path.Path, opts ArgParseMode) error {
			return S3.SetNotification(opts[AddedArgs], dst.Bucket)
		}),
	}
}

func EventGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "get [alias:bucket] ...",
		Short:             "Print bucket(s) event notification configuration (JSON)",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.GetNotification(dst.Bucket)
		}),
	}
}

func EventDelCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "del [alias:bucket] ...",
		Short:             "Remove all bucket event notification configurations",
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.DelNotification(dst.Bucket)
		}),
	}
}
