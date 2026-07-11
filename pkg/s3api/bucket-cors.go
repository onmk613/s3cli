package s3api

import (
	"context"
	"io"
	"s3cli/pkg/s3api/cors"
)

// SetBucketCors 设置指定 bucket 的 CORS 配置.
func (c *Client) SetBucketCors(ctx context.Context, bucketName string, config *cors.Config) error {
	body, err := config.ToXML()
	if err != nil {
		return err
	}
	return c.putBucketSubresource(ctx, bucketName, "cors", body)
}

// GetBucketCors 获取指定 bucket 的 CORS 配置.
func (c *Client) GetBucketCors(ctx context.Context, bucketName string) (*cors.Config, error) {
	resp, err := c.getBucketSubresource(ctx, bucketName, "cors")
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	return cors.ParseBucketCorsConfig(resp.Body)
}

// DeleteBucketCors 删除指定 bucket 的 CORS 配置.
func (c *Client) DeleteBucketCors(ctx context.Context, bucketName string) error {
	return c.deleteBucketSubresource(ctx, bucketName, "cors")
}
