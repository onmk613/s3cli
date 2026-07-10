package s3api

import (
	"context"
	"encoding/xml"
)

// ServerSideEncryptionByDefault 描述默认加密算法.
type ServerSideEncryptionByDefault struct {
	XMLName        xml.Name `xml:"ApplyServerSideEncryptionByDefault"`
	SSEAlgorithm   string   `xml:"SSEAlgorithm"` // AES256 / aws:kms
	KMSMasterKeyID string   `xml:"KMSMasterKeyID,omitempty"`
}

// ServerSideEncryptionRule 单条加密规则.
type ServerSideEncryptionRule struct {
	XMLName                            xml.Name                      `xml:"Rule"`
	ApplyServerSideEncryptionByDefault ServerSideEncryptionByDefault `xml:"ApplyServerSideEncryptionByDefault"`
	BucketKeyEnabled                   *bool                         `xml:"BucketKeyEnabled,omitempty"`
}

// ServerSideEncryptionConfiguration 加密配置.
type ServerSideEncryptionConfiguration struct {
	XMLName xml.Name                   `xml:"ServerSideEncryptionConfiguration"`
	Rules   []ServerSideEncryptionRule `xml:"Rule"`
}

// SetBucketEncryption 设置 bucket 的默认加密配置.
func (c *Client) SetBucketEncryption(ctx context.Context, bucket string, config *ServerSideEncryptionConfiguration) error {
	body, err := marshalXMLWithHeader(config)
	if err != nil {
		return err
	}
	return c.putBucketSubresource(ctx, bucket, "encryption", body)
}

// GetBucketEncryption 获取 bucket 的默认加密配置.
//
// 若 bucket 无加密配置, S3 返回 ServerSideEncryptionConfigurationNotFoundError 错误.
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
