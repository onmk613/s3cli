// bucket-notification_test.go 覆盖 `bucket event set/get` 的端到端路径:
// AWS CLI 兼容 JSON --(action 加载)--> DTO --(XML 线协议)--> 服务端 --(XML)--> DTO.
//
// 回归背景: 通知 DTO 此前没有 json tag, 用 aws s3api 导出的标准 JSON (LambdaFunctionArn /
// Filter.Key) 会被忽略, 报"未找到通知配置"。

package action

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"s3cli/internal/api"
)

// testNotificationAction 构造一个指向内存 mock 服务端的 Action, 并返回 mock 以便
// 检查服务端实际收到的 XML。
func testNotificationAction(t *testing.T) (*Action, *api.Client, *mockS3Server) {
	t.Helper()
	mock := newMockS3Server()
	server := httptest.NewServer(mock)
	t.Cleanup(server.Close)

	cli, err := api.New(&api.Options{
		Endpoint:   server.URL,
		AccessKey:  "access",
		SecretKey:  "secret",
		MaxRetries: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &Action{S3: cli, Alias: "test", Ctx: context.Background()}, cli, mock
}

const awsCLIEventJSON = `{
  "QueueConfigurations": [
    {
      "Id": "q1",
      "QueueArn": "arn:aws:sqs:us-east-1:1:queue",
      "Events": ["s3:ObjectCreated:*"],
      "Filter": {"Key": {"FilterRules": [{"Name": "prefix", "Value": "logs/"}]}}
    }
  ],
  "LambdaFunctionConfigurations": [
    {
      "Id": "l1",
      "LambdaFunctionArn": "arn:aws:lambda:us-east-1:1:function:fn",
      "Events": ["s3:ObjectCreated:Put"]
    }
  ]
}`

func TestSetNotificationAcceptsAWSCLIJSON(t *testing.T) {
	a, cli, mock := testNotificationAction(t)

	path := filepath.Join(t.TempDir(), "events.json")
	if err := os.WriteFile(path, []byte(awsCLIEventJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.SetNotification(path, "mybucket"); err != nil {
		t.Fatalf("AWS CLI 形状的 JSON 应被接受: %v", err)
	}

	// 服务端收到的 XML: 默认命名 (CloudFunctionConfiguration/CloudFunction),
	// 过滤条件落在 <S3Key><FilterRule> (JSON 侧写作 Filter.Key.FilterRules)。
	mock.mu.Lock()
	sent := string(mock.notifications["mybucket"])
	mock.mu.Unlock()
	for _, want := range []string{
		"<CloudFunctionConfiguration>", "<CloudFunction>arn:aws:lambda:us-east-1:1:function:fn</CloudFunction>",
		"<QueueConfiguration>", "<Queue>arn:aws:sqs:us-east-1:1:queue</Queue>",
		"<S3Key>", `<Name>prefix</Name>`, `<Value>logs/</Value>`,
	} {
		if !strings.Contains(sent, want) {
			t.Errorf("发出的 XML 缺少 %s:\n%s", want, sent)
		}
	}
	if strings.Contains(sent, "<Key>") {
		t.Errorf("XML 不应出现 JSON 侧的 Key 元素:\n%s", sent)
	}

	// 再读回: XML -> DTO, 验证整条链路自洽
	got, err := cli.GetBucketNotification(context.Background(), "mybucket")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.QueueConfigurations) != 1 || got.QueueConfigurations[0].QueueARN != "arn:aws:sqs:us-east-1:1:queue" {
		t.Errorf("队列配置未往返: %+v", got.QueueConfigurations)
	}
	if len(got.LambdaFunctionConfigurations) != 1 ||
		got.LambdaFunctionConfigurations[0].LambdaARN != "arn:aws:lambda:us-east-1:1:function:fn" {
		t.Errorf("Lambda 配置未往返: %+v", got.LambdaFunctionConfigurations)
	}
	if f := got.QueueConfigurations[0].Filter; f == nil || len(f.S3Key.FilterRules) != 1 ||
		f.S3Key.FilterRules[0].Name != "prefix" || f.S3Key.FilterRules[0].Value != "logs/" {
		t.Errorf("过滤规则未往返: %+v", got.QueueConfigurations[0].Filter)
	}
}

// TestSetNotificationRejectsIncompleteConfig JSON 解析对未知字段宽容, 键名拼错会留下空 ARN;
// 必须在本地拦住并指明是哪一类配置, 而不是把空 ARN 发给服务端换回一个 400。
func TestSetNotificationRejectsIncompleteConfig(t *testing.T) {
	a, _, _ := testNotificationAction(t)

	cases := []struct {
		name, body, want string
	}{
		{
			name: "ARN 键名拼错",
			body: `{"LambdaFunctionConfigurations":[{"Id":"l1","LambdArn":"arn:x","Events":["s3:ObjectCreated:*"]}]}`,
			want: "Lambda",
		},
		{
			name: "缺少 Events",
			body: `{"QueueConfigurations":[{"Id":"q1","QueueArn":"arn:aws:sqs:us-east-1:1:queue"}]}`,
			want: "Events",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "bad.json")
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			err := a.SetNotification(path, "mybucket")
			if err == nil {
				t.Fatal("应报错")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("错误信息应包含 %q: %v", tc.want, err)
			}
		})
	}
}

// TestSetNotificationRejectsUnrecognizedJSON 顶层键名全不认识时, 错误信息需点名受支持的键。
func TestSetNotificationRejectsUnrecognizedJSON(t *testing.T) {
	a, _, _ := testNotificationAction(t)

	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte(`{"LambdaConfigurations":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	err := a.SetNotification(path, "mybucket")
	if err == nil {
		t.Fatal("未识别的键应报错")
	}
	if !strings.Contains(err.Error(), "LambdaFunctionConfigurations") {
		t.Errorf("错误信息应点名受支持的键: %v", err)
	}
}
