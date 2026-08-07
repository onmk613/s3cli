// backend_switch_test.go 用自建请求后端 (api.Client) 跑双后端一致性断言.
// 本文件基于内存 mock 服务端验证 api 后端的核心操作语义.

package action

import (
	"net/http/httptest"
	"testing"

	"s3cli/pkg/api"
	"s3cli/pkg/s3iface"
)

// TestActionParityS3API 用 api.Client 后端跑 runParityScenarios.
func TestActionParityS3API(t *testing.T) {
	server := httptest.NewServer(newMockS3Server())
	defer server.Close()

	builtin, err := api.New(&api.Options{
		Endpoint:   server.URL,
		AccessKey:  "access",
		SecretKey:  "secret",
		MaxRetries: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	runParityScenarios(t, "api-builtin", s3iface.S3Operations(builtin))
}
