package api

import (
	"context"
	"encoding/xml"
)

// ServerSideEncryptionByDefault 描述默认加密算法.
type ServerSideEncryptionByDefault struct {
	XMLName        xml.Name `xml:"ApplyServerSideEncryptionByDefault" json:"-"`
	SSEAlgorithm   string   `xml:"SSEAlgorithm" json:"SSEAlgorithm"`
	KMSMasterKeyID string   `xml:"KMSMasterKeyID,omitempty" json:"KMSMasterKeyID,omitempty"`
}

// ServerSideEncryptionRule 单条加密规则.
type ServerSideEncryptionRule struct {
	XMLName                            xml.Name                      `xml:"Rule" json:"-"`
	ApplyServerSideEncryptionByDefault ServerSideEncryptionByDefault `xml:"ApplyServerSideEncryptionByDefault" json:"ApplyServerSideEncryptionByDefault"`
	BucketKeyEnabled                   *bool                         `xml:"BucketKeyEnabled,omitempty" json:"BucketKeyEnabled,omitempty"`
}

// ServerSideEncryptionConfiguration 加密配置.
type ServerSideEncryptionConfiguration struct {
	XMLName xml.Name                   `xml:"ServerSideEncryptionConfiguration" json:"-"`
	Rules   []ServerSideEncryptionRule `xml:"Rule" json:"Rules"`
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
