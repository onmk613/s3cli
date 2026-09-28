// bucket-cors_test.go 覆盖 `bucket cors set --from-file/get` 的端到端路径:
// AWS CLI 兼容 JSON --(action 加载)--> DTO --(XML 线协议)--> 服务端 --(XML)--> DTO。
//
// 回归背景: CorsRule 的 json tag 曾是单数 (AllowedOrigin), 与 AWS CLI 的复数键
// (AllowedOrigins) 不一致, 于是从 aws s3api 复制来的 AWS 风格 JSON 会被解析成空壳规则
// (只剩 ID/MaxAgeSeconds), 却一路"成功"发到服务端。

package action

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const awsCLICorsJSON = `{
  "CORSRules": [
    {
      "ID": "rule1",
      "AllowedOrigins": ["https://example.com"],
      "AllowedMethods": ["GET", "PUT"],
      "AllowedHeaders": ["Authorization"],
      "ExposeHeaders": ["ETag"],
      "MaxAgeSeconds": 600
    }
  ]
}`

func TestSetCorsFromFileAcceptsAWSCLIJSON(t *testing.T) {
	a, cli, mock := testNotificationAction(t) // 同一套 mock 服务端

	path := filepath.Join(t.TempDir(), "cors.json")
	if err := os.WriteFile(path, []byte(awsCLICorsJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.SetCors(CorsOptions{ConfigFile: path}, "mybucket"); err != nil {
		t.Fatalf("AWS CLI 形状的 CORS JSON 应被接受: %v", err)
	}

	// 服务端收到的 XML: 列表在 XML 中是逐个单数元素
	mock.mu.Lock()
	sent := string(mock.cors["mybucket"])
	mock.mu.Unlock()
	for _, want := range []string{
		"<AllowedOrigin>https://example.com</AllowedOrigin>",
		"<AllowedMethod>GET</AllowedMethod>", "<AllowedMethod>PUT</AllowedMethod>",
		"<AllowedHeader>Authorization</AllowedHeader>",
		"<ExposeHeader>ETag</ExposeHeader>",
		"<MaxAgeSeconds>600</MaxAgeSeconds>",
	} {
		if !strings.Contains(sent, want) {
			t.Errorf("发出的 XML 缺少 %s:\n%s", want, sent)
		}
	}

	// 读回并校验
	cfg, err := cli.GetBucketCors(context.Background(), "mybucket")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.CORSRules) != 1 {
		t.Fatalf("应读回 1 条规则: %+v", cfg.CORSRules)
	}
	got := cfg.CORSRules[0]
	if len(got.AllowedOrigin) != 1 || got.AllowedOrigin[0] != "https://example.com" {
		t.Errorf("AllowedOrigins 未往返: %+v", got.AllowedOrigin)
	}
	if len(got.AllowedMethod) != 2 {
		t.Errorf("AllowedMethods 未往返: %+v", got.AllowedMethod)
	}
}

// TestSetCorsFromFileRejectsEmptyRule 键名拼错或漏写时, 规则会只剩空壳;
// 必须在本地报出是第几条规则缺什么, 而不是把空规则发给服务端。
func TestSetCorsFromFileRejectsEmptyRule(t *testing.T) {
	a, _, _ := testNotificationAction(t)

	cases := []struct {
		name, body, want string
	}{
		{
			name: "单数键名 (旧式写法)",
			body: `{"CORSRules":[{"ID":"r1","AllowedOrigin":["https://a.example"],"AllowedMethods":["GET"]}]}`,
			want: "AllowedOrigins",
		},
		{
			name: "缺少 AllowedMethods",
			body: `{"CORSRules":[{"ID":"r2","AllowedOrigins":["https://a.example"]}]}`,
			want: "AllowedMethods",
		},
		{
			name: "规则数组键名错误",
			body: `{"Rules":[{"AllowedOrigins":["https://a.example"],"AllowedMethods":["GET"]}]}`,
			want: "CORSRules",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "cors.json")
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			err := a.SetCors(CorsOptions{ConfigFile: path}, "mybucket")
			if err == nil {
				t.Fatal("应报错")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("错误信息应包含 %q: %v", tc.want, err)
			}
		})
	}
}

// TestGetCorsOutputCanBeFedBack `cors get` 的输出应是 AWS CLI 形状, 可直接回灌 set。
func TestGetCorsOutputCanBeFedBack(t *testing.T) {
	a, _, _ := testNotificationAction(t)

	path := filepath.Join(t.TempDir(), "cors.json")
	if err := os.WriteFile(path, []byte(awsCLICorsJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.SetCors(CorsOptions{ConfigFile: path}, "mybucket"); err != nil {
		t.Fatal(err)
	}

	cfg, err := a.S3.GetBucketCors(context.Background(), "mybucket")
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	roundTrip := filepath.Join(t.TempDir(), "again.json")
	if err := os.WriteFile(roundTrip, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.SetCors(CorsOptions{ConfigFile: roundTrip}, "mybucket"); err != nil {
		t.Fatalf("get 的输出应能回灌 set: %v", err)
	}
}
