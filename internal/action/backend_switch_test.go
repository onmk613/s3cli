package action

import (
	"net/http/httptest"
	"testing"

	"github.com/onmk613/s3cli/pkg/api"
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

	runParityScenarios(t, "api-builtin", builtin)
}
