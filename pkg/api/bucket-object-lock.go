package api

import (
	"context"
	"encoding/xml"
)

// ObjectLockConfiguration 桶级 Object Lock 配置 (含默认保留规则).
type ObjectLockConfiguration struct {
	XMLName           xml.Name        `xml:"ObjectLockConfiguration" json:"-"`
	ObjectLockEnabled string          `xml:"ObjectLockEnabled,omitempty" json:"ObjectLockEnabled,omitempty"` // "Enabled"
	Rule              *ObjectLockRule `xml:"Rule,omitempty" json:"Rule,omitempty"`
}

// ObjectLockRule Object Lock 默认保留规则容器.
type ObjectLockRule struct {
	DefaultRetention DefaultRetention `xml:"DefaultRetention" json:"DefaultRetention"`
}

// DefaultRetention 默认保留设置.
type DefaultRetention struct {
	Mode  string `xml:"Mode" json:"Mode"` // GOVERNANCE / COMPLIANCE
	Days  int    `xml:"Days,omitempty" json:"Days,omitempty"`
	Years int    `xml:"Years,omitempty" json:"Years,omitempty"`
}

// GetObjectLockConfiguration 获取 bucket 的 Object Lock 配置.
func (c *Client) GetObjectLockConfiguration(ctx context.Context, bucket string) (*ObjectLockConfiguration, error) {
	var result ObjectLockConfiguration
	if err := c.getBucketSubresourceXML(ctx, bucket, "object-lock", &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// PutObjectLockConfiguration 设置 bucket 的 Object Lock 配置 (含默认保留规则).
func (c *Client) PutObjectLockConfiguration(ctx context.Context, bucket string, config *ObjectLockConfiguration) error {
	body, err := marshalXMLWithHeader(config)
	if err != nil {
		return err
	}
	return c.putBucketSubresource(ctx, bucket, "object-lock", body)
}
