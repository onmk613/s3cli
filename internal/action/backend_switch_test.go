//go:build !aws

// backend_switch_test.go 用自建请求后端 (s3api.Client) 跑双后端一致性断言.
// 本文件仅在默认构建 (无 -tags aws) 下编译; 官方 SDK 后端见 backend_switch_aws_test.go.

package action

import (
	"net/http/httptest"
	"testing"

	"s3cli/pkg/s3api"
	"s3cli/pkg/s3iface"
)

// TestActionParityS3API 用 s3api.Client 后端跑 runParityScenarios.
func TestActionParityS3API(t *testing.T) {
	server := httptest.NewServer(newMockS3Server())
	defer server.Close()

	builtin, err := s3api.New(&s3api.Options{
		Endpoint:   server.URL,
		AccessKey:  "access",
		SecretKey:  "secret",
		MaxRetries: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	runParityScenarios(t, "s3api-builtin", s3iface.S3Operations(builtin))
}
