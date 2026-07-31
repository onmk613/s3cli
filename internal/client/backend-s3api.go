//go:build !aws

// backend-s3api.go 编译期选择自建请求后端: newBackendClient 构造 s3api.Client.
// 构建默认 (无 -tags aws) 时生效.

package client

import (
	"context"

	"s3cli/internal/config"
	"s3cli/pkg/s3iface"
)

// newBackendClient 构造自建的 s3api.Client.
func newBackendClient(ctx context.Context, static config.Static, flags config.Flags) (s3iface.S3Operations, error) {
	return NewS3Client(ctx, static, flags)
}
