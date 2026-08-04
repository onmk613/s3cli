// bucket-lifecycle.go 实现桶的生命周期配置管理: Set/Get/DeleteBucketLifecycle.
// LifecycleConfig 等类型定义在中立包 s3iface, 此处仅含操作逻辑.

package s3api

import (
	"context"
	"io"
)

// SetBucketLifecycle 设置 bucket 的生命周期配置.
func (c *Client) SetBucketLifecycle(ctx context.Context, bucket string, config *LifecycleConfig) error {
	body, err := config.ToXML()
	if err != nil {
		return err
	}
	return c.putBucketSubresource(ctx, bucket, "lifecycle", body)
}

// GetBucketLifecycle 获取 bucket 的生命周期配置.
func (c *Client) GetBucketLifecycle(ctx context.Context, bucket string) (*LifecycleConfig, error) {
	resp, err := c.getBucketSubresource(ctx, bucket, "lifecycle")
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	return ParseBucketLifecycleConfig(resp.Body)
}

// DeleteBucketLifecycle 删除 bucket 的生命周期配置.
func (c *Client) DeleteBucketLifecycle(ctx context.Context, bucket string) error {
	return c.deleteBucketSubresource(ctx, bucket, "lifecycle")
}
