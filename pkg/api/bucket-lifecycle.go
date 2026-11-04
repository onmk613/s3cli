package api

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
)

// LifecycleConfig 是 bucket 生命周期配置.
//
// 注意: 不声明 XMLName 根元素约束 —— 服务端返回的根元素不统一
// (有的返回 <LifecycleConfiguration>, 有的返回
// <BucketLifecycleConfiguration>), 去掉约束即可同时兼容;
// 序列化时由 ToXML 显式包裹为 <LifecycleConfiguration> (PUT 的标准根元素).
type LifecycleConfig struct {
	XMLNS string          `xml:"xmlns,attr,omitempty" json:"-"`
	Rules []LifecycleRule `xml:"Rule" json:"Rules,omitempty"`
}

// LifecycleRule 单条生命周期规则.
type LifecycleRule struct {
	XMLName                        xml.Name                        `xml:"Rule" json:"-"`
	ID                             string                          `xml:"ID,omitempty" json:"ID,omitempty"`
	Status                         string                          `xml:"Status" json:"Status"`
	Filter                         *Filter                         `xml:"Filter,omitempty" json:"Filter,omitempty"`
	Transitions                    []Transition                    `xml:"Transition,omitempty" json:"Transition,omitempty"`
	Expiration                     *Expiration                     `xml:"Expiration,omitempty" json:"Expiration,omitempty"`
	NoncurrentVersionExpiration    *NoncurrentVersionExpiration    `xml:"NoncurrentVersionExpiration,omitempty" json:"NoncurrentVersionExpiration,omitempty"`
	NoncurrentVersionTransitions   []NoncurrentVersionTransition   `xml:"NoncurrentVersionTransition,omitempty" json:"NoncurrentVersionTransitions,omitempty"`
	AbortIncompleteMultipartUpload *AbortIncompleteMultipartUpload `xml:"AbortIncompleteMultipartUpload,omitempty" json:"AbortIncompleteMultipartUpload,omitempty"`
}

// Filter 过滤规则.
type Filter struct {
	XMLName               xml.Name `xml:"Filter" json:"-"`
	Prefix                string   `xml:"Prefix,omitempty" json:"Prefix,omitempty"`
	Tag                   *Tag     `xml:"Tag,omitempty" json:"Tag,omitempty"`
	And                   *And     `xml:"And,omitempty" json:"And,omitempty"`
	ObjectSizeLessThan    *int64   `xml:"ObjectSizeLessThan,omitempty" json:"ObjectSizeLessThan,omitempty"`
	ObjectSizeGreaterThan *int64   `xml:"ObjectSizeGreaterThan,omitempty" json:"ObjectSizeGreaterThan,omitempty"`
}

// Tag 标签过滤.
type Tag struct {
	XMLName xml.Name `xml:"Tag" json:"-"`
	Key     string   `xml:"Key" json:"Key"`
	Value   string   `xml:"Value" json:"Value"`
}

// And 组合过滤条件.
type And struct {
	XMLName               xml.Name `xml:"And" json:"-"`
	Prefix                string   `xml:"Prefix,omitempty" json:"Prefix,omitempty"`
	Tags                  []Tag    `xml:"Tag,omitempty" json:"Tags,omitempty"`
	ObjectSizeLessThan    *int64   `xml:"ObjectSizeLessThan,omitempty" json:"ObjectSizeLessThan,omitempty"`
	ObjectSizeGreaterThan *int64   `xml:"ObjectSizeGreaterThan,omitempty" json:"ObjectSizeGreaterThan,omitempty"`
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
	XMLName                   xml.Name `xml:"Expiration" json:"-"`
	Days                      *int     `xml:"Days,omitempty" json:"Days,omitempty"`
	Date                      string   `xml:"Date,omitempty" json:"Date,omitempty"`
	ExpiredObjectDeleteMarker *bool    `xml:"ExpiredObjectDeleteMarker,omitempty" json:"ExpiredObjectDeleteMarker,omitempty"`
	// ExpiredObjectAllVersions 由 --expire-all-object-versions 生成.
	ExpiredObjectAllVersions *bool `xml:"ExpiredObjectAllVersions,omitempty" json:"ExpiredObjectAllVersions,omitempty"`
}

// NoncurrentVersionExpiration 非当前版本过期.
type NoncurrentVersionExpiration struct {
	XMLName                 xml.Name `xml:"NoncurrentVersionExpiration" json:"-"`
	NoncurrentDays          *int     `xml:"NoncurrentDays,omitempty" json:"NoncurrentDays,omitempty"`
	NewerNoncurrentVersions *int     `xml:"NewerNoncurrentVersions,omitempty" json:"NewerNoncurrentVersions,omitempty"`
}

// NoncurrentVersionTransition 非当前版本过渡.
type NoncurrentVersionTransition struct {
	XMLName                 xml.Name `xml:"NoncurrentVersionTransition" json:"-"`
	NoncurrentDays          *int     `xml:"NoncurrentDays,omitempty" json:"NoncurrentDays,omitempty"`
	NewerNoncurrentVersions *int     `xml:"NewerNoncurrentVersions,omitempty" json:"NewerNoncurrentVersions,omitempty"`
	StorageClass            string   `xml:"StorageClass" json:"StorageClass"`
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
		c.XMLNS = DefaultXMLNS
	}
	return &c, nil
}

// DefaultLifecycleRootElement 是 PUT ?lifecycle 的标准根元素名.
const DefaultLifecycleRootElement = "LifecycleConfiguration"

// ToXML 将生命周期配置序列化为 XML, 根元素固定为 <LifecycleConfiguration>.
func (c *LifecycleConfig) ToXML() ([]byte, error) {
	return marshalLifecycleXML(c, "", "")
}

// marshalLifecycleXML 序列化生命周期配置, 允许覆盖根元素名与命名空间.
// root 为空时用 DefaultLifecycleRootElement; xmlns 为空时沿用 c.XMLNS, 仍为空则用 DefaultXMLNS.
// 厂商差异 (如要求 <BucketLifecycleConfiguration>) 由 Quirks 经此处注入, 无需改类型定义.
func marshalLifecycleXML(c *LifecycleConfig, root, xmlns string) ([]byte, error) {
	if root == "" {
		root = DefaultLifecycleRootElement
	}
	if xmlns == "" {
		xmlns = c.XMLNS
	}
	if xmlns == "" {
		xmlns = DefaultXMLNS
	}
	// XMLName 不写 tag: 根元素名由运行期值决定, 便于按厂商覆盖.
	wrapper := struct {
		XMLName xml.Name
		XMLNS   string          `xml:"xmlns,attr,omitempty"`
		Rules   []LifecycleRule `xml:"Rule,omitempty"`
	}{
		XMLName: xml.Name{Local: root},
		XMLNS:   xmlns,
		Rules:   c.Rules,
	}
	data, err := xml.Marshal(wrapper)
	if err != nil {
		return nil, fmt.Errorf("marshaling lifecycle xml: %w", err)
	}
	return append([]byte(xml.Header), data...), nil
}

// SetBucketLifecycle 设置 bucket 的生命周期配置.
func (c *Client) SetBucketLifecycle(ctx context.Context, bucket string, config *LifecycleConfig) error {
	body, err := marshalLifecycleXML(config, c.quirks.LifecycleRootElement, c.quirks.XMLNS)
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
