package api

import (
	"context"
	"encoding/xml"
)

// 事件通知
//
// 这些类型同时承载两种线格式, 字段名按各自规范独立标注:
//   - XML (S3 线协议): <CloudFunctionConfiguration>/<CloudFunction> 等历史命名
//   - JSON (AWS CLI / SDK 兼容): LambdaFunctionArn / QueueArn / TopicArn,
//     过滤条件为 Filter.Key.FilterRules
//
// 因此 `bucket event get` 输出的 JSON 可直接回灌 `bucket event set`, 也可直接用于
// aws s3api put-bucket-notification-configuration。回归测试见
// bucket-notification-json_test.go。

// TopicConfiguration 主题通知配置.
type TopicConfiguration struct {
	XMLName  xml.Name            `xml:"TopicConfiguration" json:"-"`
	ID       string              `xml:"Id,omitempty" json:"Id,omitempty"`
	TopicARN string              `xml:"Topic" json:"TopicArn,omitempty"`
	Events   []string            `xml:"Event" json:"Events,omitempty"`
	Filter   *NotificationFilter `xml:"Filter,omitempty" json:"Filter,omitempty"`
}

// QueueConfiguration 队列通知配置.
type QueueConfiguration struct {
	XMLName  xml.Name            `xml:"QueueConfiguration" json:"-"`
	ID       string              `xml:"Id,omitempty" json:"Id,omitempty"`
	QueueARN string              `xml:"Queue" json:"QueueArn,omitempty"`
	Events   []string            `xml:"Event" json:"Events,omitempty"`
	Filter   *NotificationFilter `xml:"Filter,omitempty" json:"Filter,omitempty"`
}

// LambdaFunctionConfiguration Lambda 函数通知配置.
type LambdaFunctionConfiguration struct {
	XMLName   xml.Name            `xml:"CloudFunctionConfiguration" json:"-"`
	ID        string              `xml:"Id,omitempty" json:"Id,omitempty"`
	LambdaARN string              `xml:"CloudFunction" json:"LambdaFunctionArn,omitempty"`
	Events    []string            `xml:"Event" json:"Events,omitempty"`
	Filter    *NotificationFilter `xml:"Filter,omitempty" json:"Filter,omitempty"`
}

// NotificationFilter 通知过滤规则.
//
// S3Key 字段在 XML 中写作 <S3Key>, 在 AWS CLI JSON 中写作 "Key"
// (对应 SDK 模型的 NotificationConfigurationFilter.Key), 故两种 tag 名不同。
type NotificationFilter struct {
	XMLName xml.Name `xml:"Filter" json:"-"`
	S3Key   struct {
		XMLName     xml.Name     `xml:"S3Key" json:"-"`
		FilterRules []FilterRule `xml:"FilterRule" json:"FilterRules,omitempty"`
	} `xml:"S3Key" json:"Key"`
}

// FilterRule 单条过滤规则.
type FilterRule struct {
	XMLName xml.Name `xml:"FilterRule" json:"-"`
	Name    string   `xml:"Name" json:"Name,omitempty"`
	Value   string   `xml:"Value" json:"Value,omitempty"`
}

// NotificationConfiguration 桶事件通知配置.
//
// 写方向: 默认使用与 AWS 线协议一致的 <CloudFunctionConfiguration>/<CloudFunction>,
// 可经 Quirks.LambdaNotificationElement 切换为 <LambdaFunctionConfiguration>/<LambdaFunction>.
// 读方向: UnmarshalXML 始终同时接受两种命名, 见下方实现.
// JSON 方向: 字段名与 AWS CLI 一致 (LambdaFunctionConfigurations 等), XMLName 不参与 JSON.
type NotificationConfiguration struct {
	XMLName                      xml.Name                      `xml:"NotificationConfiguration" json:"-"`
	TopicConfigurations          []TopicConfiguration          `xml:"TopicConfiguration,omitempty" json:"TopicConfigurations,omitempty"`
	QueueConfigurations          []QueueConfiguration          `xml:"QueueConfiguration,omitempty" json:"QueueConfigurations,omitempty"`
	LambdaFunctionConfigurations []LambdaFunctionConfiguration `xml:"CloudFunctionConfiguration,omitempty" json:"LambdaFunctionConfigurations,omitempty"`
}

// lambdaFunctionConfigurationModern 是 Lambda 通知的现代命名变体, 仅用于序列化:
// LambdaFunctionConfiguration 的 XMLName tag 固定为 CloudFunctionConfiguration,
// 会覆盖外层字段 tag, 因此需要独立的类型承载 <LambdaFunctionConfiguration>.
type lambdaFunctionConfigurationModern struct {
	XMLName   xml.Name            `xml:"LambdaFunctionConfiguration"`
	ID        string              `xml:"Id,omitempty"`
	LambdaARN string              `xml:"LambdaFunction"`
	Events    []string            `xml:"Event"`
	Filter    *NotificationFilter `xml:"Filter,omitempty"`
}

// UnmarshalXML 宽容解析事件通知配置: AWS 返回 <CloudFunctionConfiguration>/<CloudFunction>,
// 部分厂商返回 <LambdaFunctionConfiguration>/<LambdaFunction>; 两者都接受, 并统一归一化到
// LambdaFunctionConfigurations (读方向放宽不改变对外行为, 故不设开关).
func (n *NotificationConfiguration) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	type raw struct {
		XMLName       xml.Name                            `xml:"NotificationConfiguration"`
		Topics        []TopicConfiguration                `xml:"TopicConfiguration"`
		Queues        []QueueConfiguration                `xml:"QueueConfiguration"`
		CloudLambdas  []LambdaFunctionConfiguration       `xml:"CloudFunctionConfiguration"`
		ModernLambdas []lambdaFunctionConfigurationModern `xml:"LambdaFunctionConfiguration"`
	}
	var r raw
	if err := d.DecodeElement(&r, &start); err != nil {
		return err
	}
	n.XMLName = r.XMLName
	n.TopicConfigurations = r.Topics
	n.QueueConfigurations = r.Queues
	n.LambdaFunctionConfigurations = r.CloudLambdas
	for _, l := range r.ModernLambdas {
		n.LambdaFunctionConfigurations = append(n.LambdaFunctionConfigurations, LambdaFunctionConfiguration{
			XMLName:   xml.Name{Local: "CloudFunctionConfiguration"},
			ID:        l.ID,
			LambdaARN: l.LambdaARN,
			Events:    l.Events,
			Filter:    l.Filter,
		})
	}
	return nil
}

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
