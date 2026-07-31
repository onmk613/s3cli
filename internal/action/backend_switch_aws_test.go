//go:build aws

// backend_switch_aws_test.go 用官方 SDK 后端 (awss3.AWS) 跑双后端一致性断言.
// 本文件仅在 -tags aws 构建下编译; 自建请求后端见 backend_switch_test.go.

package action

import (
	"context"
	"net/http/httptest"
	"testing"

	"s3cli/pkg/awss3"
	"s3cli/pkg/s3iface"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// TestActionParityAWS 用 awss3.AWS 后端跑 runParityScenarios.
func TestActionParityAWS(t *testing.T) {
	server := httptest.NewServer(newMockS3Server())
	defer server.Close()

	awsClient := s3.New(s3.Options{
		BaseEndpoint:     aws.String(server.URL),
		Region:           "us-east-1",
		Credentials:      credentials.NewStaticCredentialsProvider("access", "secret", ""),
		RetryMaxAttempts: 1,
	})
	official, err := awss3.NewAWS(context.Background(), awsClient)
	if err != nil {
		t.Fatal(err)
	}

	runParityScenarios(t, "awss3-official", s3iface.S3Operations(official))
}
