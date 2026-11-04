package api

import (
	"context"
	"encoding/xml"
)

// PublicAccessBlockConfiguration 桶级公共访问阻断配置.
type PublicAccessBlockConfiguration struct {
	XMLName               xml.Name `xml:"PublicAccessBlockConfiguration" json:"-"`
	BlockPublicAcls       bool     `xml:"BlockPublicAcls" json:"BlockPublicAcls"`
	IgnorePublicAcls      bool     `xml:"IgnorePublicAcls" json:"IgnorePublicAcls"`
	BlockPublicPolicy     bool     `xml:"BlockPublicPolicy" json:"BlockPublicPolicy"`
	RestrictPublicBuckets bool     `xml:"RestrictPublicBuckets" json:"RestrictPublicBuckets"`
}

// GetPublicAccessBlock 获取 bucket 的公共访问阻断配置.
func (c *Client) GetPublicAccessBlock(ctx context.Context, bucket string) (*PublicAccessBlockConfiguration, error) {
	var result PublicAccessBlockConfiguration
	if err := c.getBucketSubresourceXML(ctx, bucket, "publicAccessBlock", &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// PutPublicAccessBlock 设置 bucket 的公共访问阻断配置.
func (c *Client) PutPublicAccessBlock(ctx context.Context, bucket string, config *PublicAccessBlockConfiguration) error {
	body, err := marshalXMLWithHeader(config)
	if err != nil {
		return err
	}
	return c.putBucketSubresource(ctx, bucket, "publicAccessBlock", body)
}

// DeletePublicAccessBlock 删除 bucket 的公共访问阻断配置.
func (c *Client) DeletePublicAccessBlock(ctx context.Context, bucket string) error {
	return c.deleteBucketSubresource(ctx, bucket, "publicAccessBlock")
}
