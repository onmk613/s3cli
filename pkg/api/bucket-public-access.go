// bucket-public-access.go 实现桶级公共访问阻断 (Public Access Block) 配置管理.
// 类型 PublicAccessBlockConfiguration 定义在中立包 s3iface.

package api

import "context"

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
