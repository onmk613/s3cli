// bucket-cors.go 实现桶的 CORS (跨域资源共享) 配置管理: Set/Get/DeleteBucketCors.
// CorsConfig / CorsRule 类型定义在中立包 s3iface, 此处仅含操作逻辑.

package s3api

import (
	"context"
	"io"
)

// SetBucketCors 设置指定 bucket 的 CORS 配置.
func (c *Client) SetBucketCors(ctx context.Context, bucketName string, config *CorsConfig) error {
	body, err := config.ToXML()
	if err != nil {
		return err
	}
	return c.putBucketSubresource(ctx, bucketName, "cors", body)
}

// GetBucketCors 获取指定 bucket 的 CORS 配置.
func (c *Client) GetBucketCors(ctx context.Context, bucketName string) (*CorsConfig, error) {
	resp, err := c.getBucketSubresource(ctx, bucketName, "cors")
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	return ParseBucketCorsConfig(resp.Body)
}

// DeleteBucketCors 删除指定 bucket 的 CORS 配置.
func (c *Client) DeleteBucketCors(ctx context.Context, bucketName string) error {
	return c.deleteBucketSubresource(ctx, bucketName, "cors")
}
