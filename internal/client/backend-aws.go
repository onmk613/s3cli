//go:build aws

// backend-aws.go 编译期选择官方 SDK 后端: newBackendClient 构造 awss3.AWS.
// 构建时加 -tags aws 生效.

package client

import (
	"context"

	"s3cli/internal/config"
	"s3cli/pkg/s3iface"
)

// newBackendClient 构造官方 SDK 的 awss3.AWS.
func newBackendClient(ctx context.Context, static config.Static, flags config.Flags) (s3iface.S3Operations, error) {
	return NewAWSClient(ctx, static, flags)
}
