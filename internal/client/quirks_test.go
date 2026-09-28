// quirks_test.go 验证别名配置里的厂商差异开关能一路贯通到线上请求:
// config.Static --(newS3Client)--> api.Client --(实际 HTTP 请求)--> 服务端.
//
// 这是"适配新厂商 = 改配置, 不改代码"这条承诺的回归网: 任何一环断了都会在这里失败.

package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"s3cli/internal/api"
	"s3cli/internal/config"
)

func TestAliasQuirksReachTheWire(t *testing.T) {
	var shaHeader, lifecycleBody, notificationBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		shaHeader = r.Header.Get("X-Amz-Content-Sha256")
		body, _ := io.ReadAll(r.Body)
		switch {
		case r.URL.Query().Has("lifecycle"):
			lifecycleBody = string(body)
		case r.URL.Query().Has("notification"):
			notificationBody = string(body)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := config.Static{
		HostBase:                  server.URL,
		AccessKey:                 "ak",
		SecretKey:                 "sk",
		ForceUnsignedPayload:      true,
		LifecycleRootElement:      "BucketLifecycleConfiguration",
		LambdaNotificationElement: api.LambdaNotificationElementLambda,
	}
	c, err := newS3Client(cfg, config.Flags{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if _, err := c.PutObject(ctx, "bucket", "key", []byte("hello"), nil); err != nil {
		t.Fatal(err)
	}
	if shaHeader != "UNSIGNED-PAYLOAD" {
		t.Errorf("force_unsigned_payload 未生效: X-Amz-Content-Sha256 = %q", shaHeader)
	}

	if err := c.SetBucketLifecycle(ctx, "bucket", &api.LifecycleConfig{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(lifecycleBody, "<BucketLifecycleConfiguration") {
		t.Errorf("lifecycle_root_element 未生效: %s", lifecycleBody)
	}

	notif := &api.NotificationConfiguration{
		LambdaFunctionConfigurations: []api.LambdaFunctionConfiguration{
			{ID: "l1", LambdaARN: "arn:aws:lambda:fn", Events: []string{"s3:ObjectCreated:*"}},
		},
	}
	if err := c.SetBucketNotification(ctx, "bucket", notif); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(notificationBody, "<LambdaFunctionConfiguration>") {
		t.Errorf("lambda_notification_element 未生效: %s", notificationBody)
	}
}

// TestAliasQuirksDefaultIsStrict 零值配置必须保持既有 (严格 AWS) 行为, 避免升级后线上请求变化.
func TestAliasQuirksDefaultIsStrict(t *testing.T) {
	var shaHeader, lifecycleBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		shaHeader = r.Header.Get("X-Amz-Content-Sha256")
		if r.URL.Query().Has("lifecycle") {
			body, _ := io.ReadAll(r.Body)
			lifecycleBody = string(body)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c, err := newS3Client(config.Static{HostBase: server.URL, AccessKey: "ak", SecretKey: "sk"}, config.Flags{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if _, err := c.PutObject(ctx, "bucket", "key", []byte("hello"), nil); err != nil {
		t.Fatal(err)
	}
	if shaHeader == "UNSIGNED-PAYLOAD" || shaHeader == "" {
		t.Errorf("默认应为真实 payload 哈希, 得到 %q", shaHeader)
	}

	if err := c.SetBucketLifecycle(ctx, "bucket", &api.LifecycleConfig{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(lifecycleBody, "<BucketLifecycleConfiguration") || !strings.Contains(lifecycleBody, "<LifecycleConfiguration") {
		t.Errorf("默认应使用标准生命周期根元素: %s", lifecycleBody)
	}
}
