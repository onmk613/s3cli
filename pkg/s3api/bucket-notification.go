//go:build !aws

// bucket-notification.go 实现桶的事件通知配置管理: Set/Get/DeleteBucketNotification.
// NotificationConfiguration 等类型定义在中立包 s3iface.

package s3api

import (
	"context"
)

// SetBucketNotification 设置 bucket 的事件通知配置.
func (c *Client) SetBucketNotification(ctx context.Context, bucket string, config *NotificationConfiguration) error {
	body, err := marshalXMLWithHeader(config)
	if err != nil {
		return err
	}
	return c.putBucketSubresource(ctx, bucket, "notification", body)
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
