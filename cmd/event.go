package cmd

import (
	"s3cli/internal/action"
	"s3cli/internal/i18n"
	"s3cli/internal/s3path"

	"github.com/spf13/cobra"
)

// EventCmd 管理桶的事件通知配置
func EventCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "event",
		Short: i18n.T("Manage object notifications", "管理对象事件通知"),
	}
	cmd.AddCommand(EventSetCmd(), EventGetCmd(), EventDelCmd())
	return cmd
}

func EventSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set [notification-file] [alias:bucket] ...",
		Short: i18n.T("Set bucket event notifications (SQS/SNS/Lambda, JSON, AWS CLI compatible)", "设置存储桶事件通知（SQS/SNS/Lambda，JSON，兼容 AWS CLI）"),
		Long: i18n.T(
			"Set bucket event notifications from a JSON file.\n\nThe JSON uses the AWS CLI / SDK field names, so `aws s3api get-bucket-notification-configuration` output can be used as-is, and the output of `s3cli bucket event get` can be fed straight back into this command.",
			"从 JSON 文件设置存储桶事件通知。\n\nJSON 字段名与 AWS CLI / SDK 一致：`aws s3api get-bucket-notification-configuration` 的输出可直接使用，`s3cli bucket event get` 的输出也可原样回灌本命令。"),
		Example: `  # events.json
  {
    "QueueConfigurations": [
      { "Id": "q1",
        "QueueArn": "arn:aws:sqs:us-east-1:123456789012:queue",
        "Events": ["s3:ObjectCreated:*"] }
    ],
    "LambdaFunctionConfigurations": [
      { "Id": "l1",
        "LambdaFunctionArn": "arn:aws:lambda:us-east-1:123456789012:function:fn",
        "Events": ["s3:ObjectCreated:Put"],
        "Filter": { "Key": { "FilterRules": [ { "Name": "prefix", "Value": "logs/" } ] } } }
    ]
  }

  s3cli bucket event set events.json my-s3:my-bucket
  s3cli bucket event get my-s3:my-bucket > events.json   # 输出可直接回灌`,
		ValidArgsFunction: CompleteLocalFirst(AutoCompleteBucket),
		Args:              cobra.MinimumNArgs(2),
		Annotations:       FirstLocalFileOrPathMode,
		RunE: NewRunEWithMode(func(S3 action.Action, dst *s3path.Path, opts ArgParseMode) error {
			return S3.SetNotification(opts[LocalFileOrPath], dst.Bucket)
		}),
	}
}

func EventGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "get [alias:bucket] ...",
		Short:             i18n.T("Print bucket(s) event notification configuration (JSON)", "打印存储桶事件通知配置（JSON）"),
		Long:              i18n.T("Print the bucket event notification configuration as JSON.\n\nField names match the AWS CLI / SDK (LambdaFunctionArn / QueueArn / TopicArn), so the output can be fed back into `bucket event set` or used with `aws s3api put-bucket-notification-configuration`.", "以 JSON 打印存储桶事件通知配置。\n\n字段名与 AWS CLI / SDK 一致（LambdaFunctionArn / QueueArn / TopicArn），输出可直接回灌 `bucket event set`，也可用于 `aws s3api put-bucket-notification-configuration`。"),
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
		Short:             i18n.T("Remove all bucket event notification configurations", "删除存储桶所有事件通知配置"),
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.DelNotification(dst.Bucket)
		}),
	}
}
