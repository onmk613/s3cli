// bucket-encryption.go 实现桶的默认服务端加密配置管理.
// ServerSideEncryptionConfiguration 等类型定义在中立包 s3iface.

package api

import (
	"context"
)

// SetBucketEncryption 设置 bucket 的默认加密配置.
func (c *Client) SetBucketEncryption(ctx context.Context, bucket string, config *ServerSideEncryptionConfiguration) error {
	body, err := marshalXMLWithHeader(config)
	if err != nil {
		return err
	}
	return c.putBucketSubresource(ctx, bucket, "encryption", body)
}

// GetBucketEncryption 获取 bucket 的默认加密配置.
func (c *Client) GetBucketEncryption(ctx context.Context, bucket string) (*ServerSideEncryptionConfiguration, error) {
	var result ServerSideEncryptionConfiguration
	if err := c.getBucketSubresourceXML(ctx, bucket, "encryption", &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DeleteBucketEncryption 删除 bucket 的默认加密配置.
func (c *Client) DeleteBucketEncryption(ctx context.Context, bucket string) error {
	return c.deleteBucketSubresource(ctx, bucket, "encryption")
}
