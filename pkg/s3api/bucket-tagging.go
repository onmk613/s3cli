// bucket-tagging.go 实现桶标签集合管理: Set/Get/DeleteBucketTagging.
// Tagging 类型定义在中立包 s3iface (同时被 object-tagging 复用).

package s3api

import (
	"context"
	"encoding/xml"
)

// taggingConfig 对应 PutBucketTagging 请求体.
type taggingConfig struct {
	XMLName xml.Name `xml:"Tagging"`
	TagSet  struct {
		XMLName xml.Name  `xml:"TagSet"`
		Tag     []Tagging `xml:"Tag"`
	} `xml:"TagSet"`
}

// SetBucketTagging 设置 bucket 的标签集合.
func (c *Client) SetBucketTagging(ctx context.Context, bucket string, tags []Tagging) error {
	cfg := taggingConfig{}
	cfg.TagSet.Tag = tags
	body, err := marshalXMLWithHeader(&cfg)
	if err != nil {
		return err
	}
	return c.putBucketSubresource(ctx, bucket, "tagging", body)
}

// GetBucketTagging 获取 bucket 的标签集合.
//
// 若 bucket 无标签, S3 返回 NoSuchTagSet 错误.
func (c *Client) GetBucketTagging(ctx context.Context, bucket string) ([]Tagging, error) {
	var result taggingConfig
	if err := c.getBucketSubresourceXML(ctx, bucket, "tagging", &result); err != nil {
		return nil, err
	}
	return result.TagSet.Tag, nil
}

// DeleteBucketTagging 删除 bucket 的标签集合.
func (c *Client) DeleteBucketTagging(ctx context.Context, bucket string) error {
	return c.deleteBucketSubresource(ctx, bucket, "tagging")
}
