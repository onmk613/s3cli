package action

import (
	"encoding/json"
	"fmt"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/utils"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// SetNotification 设置桶事件通知 (JSON, AWS CLI 兼容)
func (c *S3Client) SetNotification(configfile, bucket string) error {
	data, format, err := utils.LoadAWSConfigFile(configfile)
	if err != nil {
		return err
	}
	if format != "json" {
		return fmt.Errorf("notification only supports JSON format (AWS CLI compatible)")
	}

	var cfg s3types.NotificationConfiguration
	if err := utils.UnmarshalAWS(data, "json", &cfg); err != nil {
		return fmt.Errorf("parse notification file %s: %w", configfile, err)
	}
	total := len(cfg.TopicConfigurations) + len(cfg.QueueConfigurations) + len(cfg.LambdaFunctionConfigurations)
	if total == 0 {
		return fmt.Errorf("no notification configurations found in %s", configfile)
	}

	_, err = c.S3.PutBucketNotificationConfiguration(c.Ctx, &s3.PutBucketNotificationConfigurationInput{
		Bucket:                    aws.String(bucket),
		NotificationConfiguration: &cfg,
	})
	if err != nil {
		return fmt.Errorf("set notification %s: %s", bucket, FormatAPIError(err))
	}

	myprint.PrintfBoldGreen("Notification set for %s %s (%d configurations)\n", c.Alias, bucket, total)
	return nil
}

// GetNotification 打印桶事件通知 (JSON)
func (c *S3Client) GetNotification(bucket string) error {
	out, err := c.S3.GetBucketNotificationConfiguration(c.Ctx, &s3.GetBucketNotificationConfigurationInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		return fmt.Errorf("get notification %s: %s", bucket, FormatAPIError(err))
	}
	m := map[string]any{
		"TopicConfigurations":          out.TopicConfigurations,
		"QueueConfigurations":          out.QueueConfigurations,
		"LambdaFunctionConfigurations": out.LambdaFunctionConfigurations,
		"EventBridgeConfiguration":     out.EventBridgeConfiguration,
	}
	b, _ := json.MarshalIndent(m, "", "  ")

	myprint.PrintfBoldBlue("# %s %s notification\n", c.Alias, bucket)
	myprint.PrintlnGreen(string(b))
	return nil
}

// DelNotification 清空桶事件通知 (写入一个空配置)
func (c *S3Client) DelNotification(bucket string) error {
	_, err := c.S3.PutBucketNotificationConfiguration(c.Ctx, &s3.PutBucketNotificationConfigurationInput{
		Bucket:                    aws.String(bucket),
		NotificationConfiguration: &s3types.NotificationConfiguration{},
	})
	if err != nil {
		return fmt.Errorf("delete notification %s: %s", bucket, FormatAPIError(err))
	}

	myprint.PrintfBoldGreen("Notification configuration cleared for %s %s\n", c.Alias, bucket)
	return nil
}
