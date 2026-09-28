// bucket-cors.go 实现桶的 CORS (跨域资源共享) 配置管理: Set/Get/DeleteBucketCors.
// CorsConfig / CorsRule 类型定义见 bucket-types.go, 此处仅含操作逻辑.

package api

import (
	"context"
	"io"
)

// SetBucketCors 设置指定 bucket 的 CORS 配置.
func (c *Client) SetBucketCors(ctx context.Context, bucketName string, config *CorsConfig) error {
	body, err := marshalCorsXML(config, c.quirks.XMLNS)
	if err != nil {
		return err
	}
	return c.putBucketSubresource(ctx, bucketName, "cors", body)
}

// marshalCorsXML 序列化 CORS 配置. xmlns 非空时覆盖配置自身的命名空间
// (厂商差异, 见 Quirks.XMLNS); 为空时保持 ToXML 的默认行为.
func marshalCorsXML(config *CorsConfig, xmlns string) ([]byte, error) {
	if xmlns == "" {
		return config.ToXML()
	}
	cp := *config
	cp.XMLNS = xmlns
	return cp.ToXML()
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
