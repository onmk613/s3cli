// bucket-object-lock.go 实现桶级 Object Lock 配置管理 (默认保留规则).
// 对象级 retention / legal-hold 见 object-lock.go.
// 类型 ObjectLockConfiguration 等定义在中立包 s3iface.

package api

import "context"

// GetObjectLockConfiguration 获取 bucket 的 Object Lock 配置.
func (c *Client) GetObjectLockConfiguration(ctx context.Context, bucket string) (*ObjectLockConfiguration, error) {
	var result ObjectLockConfiguration
	if err := c.getBucketSubresourceXML(ctx, bucket, "objectLock", &result); err != nil {
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
	return c.putBucketSubresource(ctx, bucket, "objectLock", body)
}
