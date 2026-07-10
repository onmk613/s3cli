package s3api

import (
	"context"
	"s3cli/pkg/s3api/lifecycle"
)

// SetBucketLifecycle 设置 bucket 的生命周期配置.
//
// 使用 lifecycle 子包的 Config 结构体, 与 AWS CLI 兼容.
func (c *Client) SetBucketLifecycle(ctx context.Context, bucket string, config *lifecycle.Config) error {
	body, err := config.ToXML()
	if err != nil {
		return err
	}
	return c.putBucketSubresource(ctx, bucket, "lifecycle", body)
}

// GetBucketLifecycle 获取 bucket 的生命周期配置.
//
// 若 bucket 无生命周期配置, S3 返回 NoSuchLifecycleConfiguration 错误.
func (c *Client) GetBucketLifecycle(ctx context.Context, bucket string) (*lifecycle.Config, error) {
	resp, err := c.getBucketSubresource(ctx, bucket, "lifecycle")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return lifecycle.ParseBucketLifecycleConfig(resp.Body)
}

// DeleteBucketLifecycle 删除 bucket 的生命周期配置.
func (c *Client) DeleteBucketLifecycle(ctx context.Context, bucket string) error {
	return c.deleteBucketSubresource(ctx, bucket, "lifecycle")
}
