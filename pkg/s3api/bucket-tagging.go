package s3api

import (
	"context"
	"encoding/xml"
)

// Tag 是单个标签.
type Tag struct {
	XMLName xml.Name `xml:"Tag"`
	Key     string   `xml:"Key"`
	Value   string   `xml:"Value"`
}

// taggingConfig 对应 PutBucketTagging 请求体.
type taggingConfig struct {
	XMLName xml.Name `xml:"Tagging"`
	TagSet  struct {
		XMLName xml.Name `xml:"TagSet"`
		Tag     []Tag    `xml:"Tag"`
	} `xml:"TagSet"`
}

// SetBucketTagging 设置 bucket 的标签集合.
func (c *Client) SetBucketTagging(ctx context.Context, bucket string, tags []Tag) error {
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
func (c *Client) GetBucketTagging(ctx context.Context, bucket string) ([]Tag, error) {
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
