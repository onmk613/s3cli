// bucket-tagging.go 实现桶标签集合管理: Set/Get/DeleteBucketTagging.
// Tagging 类型定义见 bucket-types.go (同时被 object-tagging 复用).

package api

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
// 若 bucket 无标签, S3 返回 NoSuchTagSet 错误. 对列表语义而言"没有标签"
// 是正常结果 (空集合), 不是失败: 否则 `tag list` 在干净桶上会以退出码 4
// 结束, 脚本无法区分"桶不存在"和"桶没有标签".
func (c *Client) GetBucketTagging(ctx context.Context, bucket string) ([]Tagging, error) {
	var result taggingConfig
	if err := c.getBucketSubresourceXML(ctx, bucket, "tagging", &result); err != nil {
		if HasCode(err, "NoSuchTagSet") {
			return nil, nil
		}
		return nil, err
	}
	return result.TagSet.Tag, nil
}

// DeleteBucketTagging 删除 bucket 的标签集合.
func (c *Client) DeleteBucketTagging(ctx context.Context, bucket string) error {
	return c.deleteBucketSubresource(ctx, bucket, "tagging")
}
