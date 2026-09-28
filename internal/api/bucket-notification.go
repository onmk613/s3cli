// bucket-notification.go 实现桶的事件通知配置管理: Set/Get/DeleteBucketNotification.
// NotificationConfiguration 等类型定义见 bucket-types.go.

package api

import (
	"context"
	"encoding/xml"
)

// SetBucketNotification 设置 bucket 的事件通知配置.
func (c *Client) SetBucketNotification(ctx context.Context, bucket string, config *NotificationConfiguration) error {
	body, err := marshalNotificationXML(config, c.quirks.LambdaNotificationElement)
	if err != nil {
		return err
	}
	return c.putBucketSubresource(ctx, bucket, "notification", body)
}

// marshalNotificationXML 序列化事件通知配置. 默认写 <CloudFunctionConfiguration>/<CloudFunction>
// (与 AWS 线协议一致); lambdaElement == LambdaNotificationElementLambda 时改写为
// <LambdaFunctionConfiguration>/<LambdaFunction> (厂商差异, 见 Quirks.LambdaNotificationElement).
// 读取方向由 NotificationConfiguration.UnmarshalXML 始终兼容两种命名.
func marshalNotificationXML(config *NotificationConfiguration, lambdaElement string) ([]byte, error) {
	if lambdaElement != LambdaNotificationElementLambda {
		return marshalXMLWithHeader(config)
	}
	modern := struct {
		XMLName     xml.Name                            `xml:"NotificationConfiguration"`
		Topics      []TopicConfiguration                `xml:"TopicConfiguration,omitempty"`
		Queues      []QueueConfiguration                `xml:"QueueConfiguration,omitempty"`
		LambdaFuncs []lambdaFunctionConfigurationModern `xml:"LambdaFunctionConfiguration,omitempty"`
	}{
		Topics: config.TopicConfigurations,
		Queues: config.QueueConfigurations,
	}
	for _, l := range config.LambdaFunctionConfigurations {
		modern.LambdaFuncs = append(modern.LambdaFuncs, lambdaFunctionConfigurationModern{
			ID:        l.ID,
			LambdaARN: l.LambdaARN,
			Events:    l.Events,
			Filter:    l.Filter,
		})
	}
	return marshalXMLWithHeader(modern)
}

// GetBucketNotification 获取 bucket 的事件通知配置.
func (c *Client) GetBucketNotification(ctx context.Context, bucket string) (*NotificationConfiguration, error) {
	var result NotificationConfiguration
	if err := c.getBucketSubresourceXML(ctx, bucket, "notification", &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DeleteBucketNotification 清空 bucket 的事件通知配置 (写入空配置).
func (c *Client) DeleteBucketNotification(ctx context.Context, bucket string) error {
	return c.SetBucketNotification(ctx, bucket, &NotificationConfiguration{})
}
