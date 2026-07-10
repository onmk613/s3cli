package s3api

import (
	"context"
	"encoding/xml"
)

// TopicConfiguration 主题通知配置.
type TopicConfiguration struct {
	XMLName  xml.Name            `xml:"TopicConfiguration"`
	ID       string              `xml:"Id,omitempty"`
	TopicARN string              `xml:"Topic"`
	Events   []string            `xml:"Event"`
	Filter   *NotificationFilter `xml:"Filter,omitempty"`
}

// QueueConfiguration 队列通知配置.
type QueueConfiguration struct {
	XMLName  xml.Name            `xml:"QueueConfiguration"`
	ID       string              `xml:"Id,omitempty"`
	QueueARN string              `xml:"Queue"`
	Events   []string            `xml:"Event"`
	Filter   *NotificationFilter `xml:"Filter,omitempty"`
}

// LambdaFunctionConfiguration Lambda 函数通知配置.
type LambdaFunctionConfiguration struct {
	XMLName   xml.Name            `xml:"CloudFunctionConfiguration"`
	ID        string              `xml:"Id,omitempty"`
	LambdaARN string              `xml:"CloudFunction"`
	Events    []string            `xml:"Event"`
	Filter    *NotificationFilter `xml:"Filter,omitempty"`
}

// NotificationFilter 通知过滤规则.
type NotificationFilter struct {
	XMLName xml.Name `xml:"Filter"`
	S3Key   struct {
		XMLName     xml.Name     `xml:"S3Key"`
		FilterRules []FilterRule `xml:"FilterRule"`
	} `xml:"S3Key"`
}

// FilterRule 单条过滤规则.
type FilterRule struct {
	XMLName xml.Name `xml:"FilterRule"`
	Name    string   `xml:"Name"` // prefix / suffix
	Value   string   `xml:"Value"`
}

// NotificationConfiguration 桶事件通知配置.
type NotificationConfiguration struct {
	XMLName                      xml.Name                      `xml:"NotificationConfiguration"`
	TopicConfigurations          []TopicConfiguration          `xml:"TopicConfiguration,omitempty"`
	QueueConfigurations          []QueueConfiguration          `xml:"QueueConfiguration,omitempty"`
	LambdaFunctionConfigurations []LambdaFunctionConfiguration `xml:"CloudFunctionConfiguration,omitempty"`
}

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
