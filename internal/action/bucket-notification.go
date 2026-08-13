// bucket-notification.go 实现桶事件通知配置管理: Set/Get/DelNotification,
// 输入为 AWS CLI 兼容的 JSON.

package action

import (
	"fmt"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/i18n"
	"s3cli/pkg/s3iface"
)

// SetNotification 设置桶事件通知 (JSON, AWS CLI 兼容)
func (c *Action) SetNotification(configure, bucket string) error {
	loaded, err := loadJSONConfig[s3iface.NotificationConfiguration](configure, "notification")
	if err != nil {
		return err
	}
	cfg := *loaded
	total := len(cfg.TopicConfigurations) + len(cfg.QueueConfigurations) + len(cfg.LambdaFunctionConfigurations)
	if total == 0 {
		return fmt.Errorf(i18n.T("no notification configurations found in %s", "在 %s 中未找到通知配置"), configure)
	}

	if err := c.S3.SetBucketNotification(c.Ctx, bucket, &cfg); err != nil {
		return fmt.Errorf("set notification %s: %s", bucket, FormatAPIError(err))
	}

	myprint.PrintfBoldGreen(i18n.T("Notification set for %s %s (%d configurations)\n", "已为 %s %s 设置通知（%d 个配置）\n"), c.Alias, bucket, total)
	return nil
}

// GetNotification 打印桶事件通知 (JSON)
func (c *Action) GetNotification(bucket string) error {
	cfg, err := c.S3.GetBucketNotification(c.Ctx, bucket)
	if err != nil {
		return fmt.Errorf("get notification %s: %s", bucket, FormatAPIError(err))
	}
	return c.printBucketConfigJSON(bucket, "notification", cfg)
}

// DelNotification 清空桶事件通知 (写入一个空配置)
func (c *Action) DelNotification(bucket string) error {
	return c.deleteBucketConfig(bucket, "notification", i18n.T("Notification configuration cleared for %s %s\n", "已为 %s %s 清空通知配置\n"),
		func() error { return c.S3.DeleteBucketNotification(c.Ctx, bucket) })
}
