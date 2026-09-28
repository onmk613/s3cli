// bucket-notification-json_test.go 锁定事件通知的 JSON 契约: 字段名与 AWS CLI / SDK
// 一致 (LambdaFunctionArn / QueueArn / TopicArn, 过滤条件为 Filter.Key.FilterRules),
// 且 XMLName 不参与 JSON。
//
// 回归背景: 通知 DTO 此前只有 xml tag, JSON 键只能写 Go 字段名 (LambdaARN / S3Key),
// 于是 aws s3api 导出的标准 JSON 会被静默忽略, 报成"未找到通知配置"。

package api

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// awsCLINotificationJSON 是 aws s3api get-bucket-notification-configuration 的输出形状。
const awsCLINotificationJSON = `{
  "TopicConfigurations": [
    {
      "Id": "t1",
      "TopicArn": "arn:aws:sns:us-east-1:123456789012:topic",
      "Events": ["s3:ObjectCreated:*"],
      "Filter": {"Key": {"FilterRules": [{"Name": "prefix", "Value": "logs/"}]}}
    }
  ],
  "QueueConfigurations": [
    {
      "Id": "q1",
      "QueueArn": "arn:aws:sqs:us-east-1:123456789012:queue",
      "Events": ["s3:ObjectRemoved:*"]
    }
  ],
  "LambdaFunctionConfigurations": [
    {
      "Id": "l1",
      "LambdaFunctionArn": "arn:aws:lambda:us-east-1:123456789012:function:fn",
      "Events": ["s3:ObjectCreated:Put"],
      "Filter": {"Key": {"FilterRules": [{"Name": "suffix", "Value": ".jpg"}]}}
    }
  ]
}`

func TestNotificationJSONAcceptsAWSCLIShape(t *testing.T) {
	// 先按 action 层的严格模式解析 (DisallowUnknownFields): AWS 形状必须一次通过,
	// 否则会退化到忽略未知字段的宽容解析, Lambda 配置被悄无声息丢掉。
	var cfg NotificationConfiguration
	dec := json.NewDecoder(strings.NewReader(awsCLINotificationJSON))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		t.Fatalf("AWS CLI 形状的 JSON 未通过严格解析: %v", err)
	}

	if len(cfg.TopicConfigurations) != 1 || cfg.TopicConfigurations[0].TopicARN != "arn:aws:sns:us-east-1:123456789012:topic" {
		t.Errorf("TopicArn 未解析: %+v", cfg.TopicConfigurations)
	}
	if len(cfg.QueueConfigurations) != 1 || cfg.QueueConfigurations[0].QueueARN != "arn:aws:sqs:us-east-1:123456789012:queue" {
		t.Errorf("QueueArn 未解析: %+v", cfg.QueueConfigurations)
	}
	if len(cfg.LambdaFunctionConfigurations) != 1 ||
		cfg.LambdaFunctionConfigurations[0].LambdaARN != "arn:aws:lambda:us-east-1:123456789012:function:fn" {
		t.Errorf("LambdaFunctionArn 未解析: %+v", cfg.LambdaFunctionConfigurations)
	}

	// Filter.Key.FilterRules -> S3Key.FilterRules
	tf := cfg.TopicConfigurations[0].Filter
	if tf == nil || len(tf.S3Key.FilterRules) != 1 {
		t.Fatalf("Topic Filter 未解析: %+v", tf)
	}
	if r := tf.S3Key.FilterRules[0]; r.Name != "prefix" || r.Value != "logs/" {
		t.Errorf("FilterRule = %+v, want {prefix logs/}", r)
	}
	lf := cfg.LambdaFunctionConfigurations[0].Filter
	if lf == nil || len(lf.S3Key.FilterRules) != 1 || lf.S3Key.FilterRules[0].Name != "suffix" {
		t.Errorf("Lambda Filter 未解析: %+v", lf)
	}
}

func TestNotificationJSONMarshalShape(t *testing.T) {
	var cfg NotificationConfiguration
	if err := json.Unmarshal([]byte(awsCLINotificationJSON), &cfg); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)

	for _, want := range []string{
		`"TopicArn"`, `"QueueArn"`, `"LambdaFunctionArn"`,
		`"Key"`, `"FilterRules"`, `"Name":"prefix"`, `"Value":"logs/"`,
		`"LambdaFunctionConfigurations"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("JSON 输出缺少 %s: %s", want, got)
		}
	}
	// 不应泄漏实现细节: XMLName 与 XML 侧命名
	for _, bad := range []string{"XMLName", "CloudFunction", `"S3Key"`, `"TopicARN"`, `"QueueARN"`, `"LambdaARN"`} {
		if strings.Contains(got, bad) {
			t.Errorf("JSON 输出泄漏 %s: %s", bad, got)
		}
	}
}

// TestNotificationJSONRoundTrip 保证 get 的输出能直接回灌 set:
// JSON -> DTO -> JSON 必须逐字节等价 (忽略字段顺序之外的差异)。
func TestNotificationJSONRoundTrip(t *testing.T) {
	var first NotificationConfiguration
	if err := json.Unmarshal([]byte(awsCLINotificationJSON), &first); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(&first)
	if err != nil {
		t.Fatal(err)
	}

	var second NotificationConfiguration
	if err := json.Unmarshal(body, &second); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Errorf("JSON 往返后不一致:\n first = %+v\nsecond = %+v", first, second)
	}

	// 再压一次确认稳定 (幂等)
	body2, err := json.Marshal(&second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, body2) {
		t.Errorf("JSON 往返不幂等:\n%s\n%s", body, body2)
	}
}

// TestNotificationJSONOmitsEmptyFields 空配置与空可选字段不应产生噪声键。
func TestNotificationJSONOmitsEmptyFields(t *testing.T) {
	out, err := json.Marshal(&NotificationConfiguration{})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(out); got != "{}" {
		t.Errorf("空配置 JSON = %s, want {}", got)
	}
	if strings.Contains(string(out), "XMLName") {
		t.Errorf("空配置泄漏 XMLName: %s", out)
	}
}
