package s3api

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
)

// SetBucketLifecycle 设置 bucket 的生命周期配置.
//
// 使用 lifecycle 子包的 Config 结构体, 与 AWS CLI 兼容.
func (c *Client) SetBucketLifecycle(ctx context.Context, bucket string, config *LifecycleConfig) error {
	body, err := config.ToXML()
	if err != nil {
		return err
	}
	return c.putBucketSubresource(ctx, bucket, "lifecycle", body)
}

// GetBucketLifecycle 获取 bucket 的生命周期配置.
//
// 若 bucket 无生命周期配置, S3 返回 NoSuchLifecycleConfiguration 错误.
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

// Config 是 bucket 生命周期配置.
// 线协议为 XML；JSON tag 用于本地配置文件读写与展示。
// XMLName/XMLNS 仅服务于 XML，对 JSON 标记为 "-"，避免污染输出。
type LifecycleConfig struct {
	XMLName xml.Name        `xml:"LifecycleConfiguration" json:"-"`
	XMLNS   string          `xml:"xmlns,attr,omitempty" json:"-"`
	Rules   []LifecycleRule `xml:"Rule" json:"Rules,omitempty"`
}

// LifecycleRule 单条生命周期规则.
type LifecycleRule struct {
	XMLName xml.Name `xml:"Rule" json:"-"`
	ID      string   `xml:"ID,omitempty" json:"ID,omitempty"`
	Status  string   `xml:"Status" json:"Status"` // Enabled / Disabled
	Filter  *Filter  `xml:"Filter,omitempty" json:"Filter,omitempty"`
	// 过渡规则: 何时转换存储类型
	Transitions []Transition `xml:"Transition,omitempty" json:"Transition,omitempty"`
	// 过期规则: 何时删除对象
	Expiration *Expiration `xml:"Expiration,omitempty" json:"Expiration,omitempty"`
	// 非当前版本过期
	NoncurrentVersionExpiration *NoncurrentVersionExpiration `xml:"NoncurrentVersionExpiration,omitempty" json:"NoncurrentVersionExpiration,omitempty"`
	// 非当前版本过渡
	NoncurrentVersionTransitions []NoncurrentVersionTransition `xml:"NoncurrentVersionTransition,omitempty" json:"NoncurrentVersionTransition,omitempty"`
	// 中止未完成的分片上传
	AbortIncompleteMultipartUpload *AbortIncompleteMultipartUpload `xml:"AbortIncompleteMultipartUpload,omitempty" json:"AbortIncompleteMultipartUpload,omitempty"`
}

// Filter 过滤规则.
type Filter struct {
	XMLName xml.Name `xml:"Filter" json:"-"`
	Prefix  string   `xml:"Prefix,omitempty" json:"Prefix,omitempty"`
	Tag     *Tag     `xml:"Tag,omitempty" json:"Tag,omitempty"`
	// And 用于组合多个条件
	And *And `xml:"And,omitempty" json:"And,omitempty"`
}

// Tag 标签过滤.
type Tag struct {
	XMLName xml.Name `xml:"Tag" json:"-"`
	Key     string   `xml:"Key" json:"Key"`
	Value   string   `xml:"Value" json:"Value"`
}

// And 组合过滤条件.
type And struct {
	XMLName xml.Name `xml:"And" json:"-"`
	Prefix  string   `xml:"Prefix,omitempty" json:"Prefix,omitempty"`
	Tags    []Tag    `xml:"Tag,omitempty" json:"Tags,omitempty"`
}

// Transition 过渡规则.
type Transition struct {
	XMLName      xml.Name `xml:"Transition" json:"-"`
	Days         *int     `xml:"Days,omitempty" json:"Days,omitempty"`
	Date         string   `xml:"Date,omitempty" json:"Date,omitempty"`
	StorageClass string   `xml:"StorageClass" json:"StorageClass"`
}

// Expiration 过期规则.
type Expiration struct {
	XMLName xml.Name `xml:"Expiration" json:"-"`
	Days    *int     `xml:"Days,omitempty" json:"Days,omitempty"`
	Date    string   `xml:"Date,omitempty" json:"Date,omitempty"`
	// ExpiredObjectDeleteMarker 仅对单版本对象有效.
	ExpiredObjectDeleteMarker *bool `xml:"ExpiredObjectDeleteMarker,omitempty" json:"ExpiredObjectDeleteMarker,omitempty"`
}

// NoncurrentVersionExpiration 非当前版本过期.
type NoncurrentVersionExpiration struct {
	XMLName        xml.Name `xml:"NoncurrentVersionExpiration" json:"-"`
	NoncurrentDays *int     `xml:"NoncurrentDays,omitempty" json:"NoncurrentDays,omitempty"`
}

// NoncurrentVersionTransition 非当前版本过渡.
type NoncurrentVersionTransition struct {
	XMLName        xml.Name `xml:"NoncurrentVersionTransition" json:"-"`
	NoncurrentDays *int     `xml:"NoncurrentDays,omitempty" json:"NoncurrentDays,omitempty"`
	StorageClass   string   `xml:"StorageClass" json:"StorageClass"`
}

// AbortIncompleteMultipartUpload 中止未完成的分片上传.
type AbortIncompleteMultipartUpload struct {
	XMLName             xml.Name `xml:"AbortIncompleteMultipartUpload" json:"-"`
	DaysAfterInitiation *int     `xml:"DaysAfterInitiation" json:"DaysAfterInitiation"`
}

// ParseBucketLifecycleConfig 从 XML 解析生命周期配置.
func ParseBucketLifecycleConfig(reader io.Reader) (*LifecycleConfig, error) {
	var c LifecycleConfig
	err := xml.NewDecoder(io.LimitReader(reader, 10*1024*1024)).Decode(&c)
	if err != nil {
		return nil, fmt.Errorf("decoding lifecycle xml: %w", err)
	}
	if c.XMLNS == "" {
		c.XMLNS = defaultXMLNS
	}
	return &c, nil
}

// ToXML 将生命周期配置序列化为 XML.
func (c *LifecycleConfig) ToXML() ([]byte, error) {
	if c.XMLNS == "" {
		c.XMLNS = defaultXMLNS
	}
	data, err := xml.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("marshaling lifecycle xml: %w", err)
	}
	return append([]byte(xml.Header), data...), nil
}
