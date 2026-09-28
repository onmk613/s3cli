// bucket-notification.go 实现桶事件通知配置管理: Set/Get/DelNotification,
// 输入为 AWS CLI 兼容的 JSON.

package action

import (
	"fmt"

	"s3cli/internal/api"
	myprint "s3cli/internal/fmtutil"
	"s3cli/internal/i18n"
)

// SetNotification 设置桶事件通知 (JSON, AWS CLI 兼容)
func (c *Action) SetNotification(configure, bucket string) error {
	loaded, err := loadJSONConfig[api.NotificationConfiguration](configure, "notification")
	if err != nil {
		return err
	}
	cfg := *loaded
	total := len(cfg.TopicConfigurations) + len(cfg.QueueConfigurations) + len(cfg.LambdaFunctionConfigurations)
	if total == 0 {
		// 未识别的字段会被忽略 (见 unmarshalAWS), 这里点名受支持的键, 便于排查拼写错误。
		return fmt.Errorf(i18n.T(
			"no notification configurations found in %s (expected TopicConfigurations / QueueConfigurations / LambdaFunctionConfigurations)",
			"在 %s 中未找到通知配置（应为 TopicConfigurations / QueueConfigurations / LambdaFunctionConfigurations）"), configure)
	}

	if err := validateNotification(&cfg); err != nil {
		return err
	}

	if err := c.S3.SetBucketNotification(c.Ctx, bucket, &cfg); err != nil {
		return fmt.Errorf("set notification %s: %w", bucket, err)
	}

	myprint.PrintfBoldGreen(i18n.T("Notification set for %s %s (%d configurations)\n", "已为 %s %s 设置通知（%d 个配置）\n"), c.Alias, bucket, total)
	return nil
}

// validateNotification 校验每条通知配置的目标 ARN 与事件列表是否齐备。
//
// JSON 解析对未识别字段是宽容的 (见 unmarshalAWS): 键名拼错只会让对应字段留空,
// 于是空 ARN 会被原样发给服务端, 换回一个难以定位的 MalformedXML/InvalidArgument。
// 在本地先报出"哪一类、哪一条配置"缺什么, 比让用户对着 400 猜要省事。
func validateNotification(cfg *api.NotificationConfiguration) error {
	check := func(kind, id, arn string, events []string) error {
		if arn == "" {
			return fmt.Errorf(i18n.T("%s notification configuration %q is missing its ARN (check the JSON field name)",
				"%s 通知配置 %q 缺少 ARN（请检查 JSON 字段名）"), kind, id)
		}
		if len(events) == 0 {
			return fmt.Errorf(i18n.T("%s notification configuration %q has no Events",
				"%s 通知配置 %q 未指定 Events"), kind, id)
		}
		return nil
	}
	for _, c := range cfg.TopicConfigurations {
		if err := check("Topic", c.ID, c.TopicARN, c.Events); err != nil {
			return err
		}
	}
	for _, c := range cfg.QueueConfigurations {
		if err := check("Queue", c.ID, c.QueueARN, c.Events); err != nil {
			return err
		}
	}
	for _, c := range cfg.LambdaFunctionConfigurations {
		if err := check("Lambda", c.ID, c.LambdaARN, c.Events); err != nil {
			return err
		}
	}
	return nil
}

// GetNotification 打印桶事件通知 (JSON)
func (c *Action) GetNotification(bucket string) error {
	cfg, err := c.S3.GetBucketNotification(c.Ctx, bucket)
	if err != nil {
		return fmt.Errorf("get notification %s: %w", bucket, err)
	}
	return c.printBucketConfigJSON(bucket, "notification", cfg)
}

// DelNotification 清空桶事件通知 (写入一个空配置)
func (c *Action) DelNotification(bucket string) error {
	return c.deleteBucketConfig(bucket, "notification", i18n.T("Notification configuration cleared for %s %s\n", "已为 %s %s 清空通知配置\n"),
		func() error { return c.S3.DeleteBucketNotification(c.Ctx, bucket) })
}
