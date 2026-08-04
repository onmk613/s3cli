// backend-s3api.go 构造自建请求后端: newBackendClient 返回 s3api.Client
// (s3iface.S3Operations 接口的唯一实现).

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
