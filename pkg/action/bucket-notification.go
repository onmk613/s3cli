package action

import (
	"fmt"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/s3api"
)

// SetNotification 设置桶事件通知 (JSON, AWS CLI 兼容)
func (c *S3Client) SetNotification(configfile, bucket string) error {
	loaded, err := loadJSONConfig[s3api.NotificationConfiguration](configfile, "notification")
	if err != nil {
		return err
	}
	cfg := *loaded
	total := len(cfg.TopicConfigurations) + len(cfg.QueueConfigurations) + len(cfg.LambdaFunctionConfigurations)
	if total == 0 {
		return fmt.Errorf("no notification configurations found in %s", configfile)
	}

	if err := c.S3.SetBucketNotification(c.Ctx, bucket, &cfg); err != nil {
		return fmt.Errorf("set notification %s: %s", bucket, FormatAPIError(err))
	}

	myprint.PrintfBoldGreen("Notification set for %s %s (%d configurations)\n", c.Alias, bucket, total)
	return nil
}

// GetNotification 打印桶事件通知 (JSON)
func (c *S3Client) GetNotification(bucket string) error {
	cfg, err := c.S3.GetBucketNotification(c.Ctx, bucket)
	if err != nil {
		return fmt.Errorf("get notification %s: %s", bucket, FormatAPIError(err))
	}
	return c.printBucketConfigJSON(bucket, "notification", cfg)
}

// DelNotification 清空桶事件通知 (写入一个空配置)
func (c *S3Client) DelNotification(bucket string) error {
	return c.deleteBucketConfig(bucket, "notification", "Notification configuration cleared for %s %s\n",
		func() error { return c.S3.DeleteBucketNotification(c.Ctx, bucket) })
}
