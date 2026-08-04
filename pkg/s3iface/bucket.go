// bucket.go 定义桶子资源配置的 DTO 类型 (CORS / 加密 / 生命周期 / 通知 / 标签 / 版本).
// 这些类型描述 S3 线协议 (XML) 与本地配置文件 (JSON) 的数据结构, 与具体后端无关.
//
// 线协议为 XML; JSON tag 用于本地配置文件读写与展示.
// XMLName/XMLNS 仅服务于 XML, 对 JSON 标记为 "-".

package s3iface

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// marshalXMLWithHeader 序列化为 XML 并加上 XML 声明头.
func marshalXMLWithHeader(v any) ([]byte, error) {
	body, err := xml.Marshal(v)
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), body...), nil
}

// ----------------------------------------------------------------------------
// CORS
// ----------------------------------------------------------------------------

// CorsConfig 是 bucket 的 CORS 配置容器.
type CorsConfig struct {
	XMLName   xml.Name   `xml:"CORSConfiguration" json:"-"`
	XMLNS     string     `xml:"xmlns,attr,omitempty" json:"-"`
	CORSRules []CorsRule `xml:"CORSRule" json:"CORSRules,omitempty"`
}

// CorsRule 是单条 CORS 规则.
type CorsRule struct {
	AllowedHeader []string `xml:"AllowedHeader,omitempty" json:"AllowedHeader,omitempty"`
	AllowedMethod []string `xml:"AllowedMethod,omitempty" json:"AllowedMethod,omitempty"`
	AllowedOrigin []string `xml:"AllowedOrigin,omitempty" json:"AllowedOrigin,omitempty"`
	ExposeHeader  []string `xml:"ExposeHeader,omitempty" json:"ExposeHeader,omitempty"`
	ID            string   `xml:"ID,omitempty" json:"ID,omitempty"`
	MaxAgeSeconds int      `xml:"MaxAgeSeconds,omitempty" json:"MaxAgeSeconds,omitempty"`
}

// ParseBucketCorsConfig 从 XML 解析 CORS 配置.
func ParseBucketCorsConfig(reader io.Reader) (*CorsConfig, error) {
	var c CorsConfig
	err := xml.NewDecoder(io.LimitReader(reader, 128*1024)).Decode(&c)
	if err != nil {
		return nil, fmt.Errorf("decoding xml: %w", err)
	}
	if c.XMLNS == "" {
		c.XMLNS = DefaultXMLNS
	}
	for i, rule := range c.CORSRules {
		for j, method := range rule.AllowedMethod {
			c.CORSRules[i].AllowedMethod[j] = strings.ToUpper(method)
		}
	}
	return &c, nil
}

// ToXML 将 CORS 配置序列化为 XML.
func (c *CorsConfig) ToXML() ([]byte, error) {
	if c.XMLNS == "" {
		c.XMLNS = DefaultXMLNS
	}
	data, err := xml.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("marshaling xml: %w", err)
	}
	return append([]byte(xml.Header), data...), nil
}

// ----------------------------------------------------------------------------
// 服务端加密
// ----------------------------------------------------------------------------

// ServerSideEncryptionByDefault 描述默认加密算法.
type ServerSideEncryptionByDefault struct {
	XMLName        xml.Name `xml:"ApplyServerSideEncryptionByDefault"`
	SSEAlgorithm   string   `xml:"SSEAlgorithm"`
	KMSMasterKeyID string   `xml:"KMSMasterKeyID,omitempty"`
}

// ServerSideEncryptionRule 单条加密规则.
type ServerSideEncryptionRule struct {
	XMLName                            xml.Name                      `xml:"Rule"`
	ApplyServerSideEncryptionByDefault ServerSideEncryptionByDefault `xml:"ApplyServerSideEncryptionByDefault"`
	BucketKeyEnabled                   *bool                         `xml:"BucketKeyEnabled,omitempty"`
}

// ServerSideEncryptionConfiguration 加密配置.
type ServerSideEncryptionConfiguration struct {
	XMLName xml.Name                   `xml:"ServerSideEncryptionConfiguration"`
	Rules   []ServerSideEncryptionRule `xml:"Rule"`
}

// ----------------------------------------------------------------------------
// 生命周期
// ----------------------------------------------------------------------------

// LifecycleConfig 是 bucket 生命周期配置.
//
// 注意: 不声明 XMLName 根元素约束 —— 服务端返回的根元素不统一
// (AWS/旧版 MinIO 为 <LifecycleConfiguration>, 新版 MinIO 为
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
	// ExpiredObjectAllVersions 由 mc 的 --expire-all-object-versions 生成 (MinIO 扩展).
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

// ToXML 将生命周期配置序列化为 XML, 根元素固定为 <LifecycleConfiguration>.
func (c *LifecycleConfig) ToXML() ([]byte, error) {
	wrapper := struct {
		XMLName xml.Name        `xml:"LifecycleConfiguration"`
		XMLNS   string          `xml:"xmlns,attr,omitempty"`
		Rules   []LifecycleRule `xml:"Rule,omitempty"`
	}{
		XMLNS: c.XMLNS,
		Rules: c.Rules,
	}
	if wrapper.XMLNS == "" {
		wrapper.XMLNS = DefaultXMLNS
	}
	data, err := xml.Marshal(wrapper)
	if err != nil {
		return nil, fmt.Errorf("marshaling lifecycle xml: %w", err)
	}
	return append([]byte(xml.Header), data...), nil
}

// ----------------------------------------------------------------------------
// 事件通知
// ----------------------------------------------------------------------------

// TopicConfiguration 主题通知配置.
type TopicConfiguration struct {
	XMLName  xml.Name            `xml:"TopicConfiguration"`
	ID       string              `xml:"Id,omitempty"`
	TopicARN string              `xml:"Topic"`
	Events   []string            `xml:"Event"`
	Filter   *NotificationFilter `xml:"Filter,omitempty"`
}

// QueueConfiguration 队列通知配置.
type QueueConfiguration struct {
	XMLName  xml.Name            `xml:"QueueConfiguration"`
	ID       string              `xml:"Id,omitempty"`
	QueueARN string              `xml:"Queue"`
	Events   []string            `xml:"Event"`
	Filter   *NotificationFilter `xml:"Filter,omitempty"`
}

// LambdaFunctionConfiguration Lambda 函数通知配置.
type LambdaFunctionConfiguration struct {
	XMLName   xml.Name            `xml:"CloudFunctionConfiguration"`
	ID        string              `xml:"Id,omitempty"`
	LambdaARN string              `xml:"CloudFunction"`
	Events    []string            `xml:"Event"`
	Filter    *NotificationFilter `xml:"Filter,omitempty"`
}

// NotificationFilter 通知过滤规则.
type NotificationFilter struct {
	XMLName xml.Name `xml:"Filter"`
	S3Key   struct {
		XMLName     xml.Name     `xml:"S3Key"`
		FilterRules []FilterRule `xml:"FilterRule"`
	} `xml:"S3Key"`
}

// FilterRule 单条过滤规则.
type FilterRule struct {
	XMLName xml.Name `xml:"FilterRule"`
	Name    string   `xml:"Name"`
	Value   string   `xml:"Value"`
}

// NotificationConfiguration 桶事件通知配置.
type NotificationConfiguration struct {
	XMLName                      xml.Name                      `xml:"NotificationConfiguration"`
	TopicConfigurations          []TopicConfiguration          `xml:"TopicConfiguration,omitempty"`
	QueueConfigurations          []QueueConfiguration          `xml:"QueueConfiguration,omitempty"`
	LambdaFunctionConfigurations []LambdaFunctionConfiguration `xml:"CloudFunctionConfiguration,omitempty"`
}

// ----------------------------------------------------------------------------
// 标签 / 版本
// ----------------------------------------------------------------------------

// Tagging 描述单个键值标签, 同时用于桶标签与对象标签.
type Tagging struct {
	XMLName xml.Name `xml:"Tag"`
	Key     string   `xml:"Key"`
	Value   string   `xml:"Value"`
}

// BucketVersioningStatus 版本控制状态.
type BucketVersioningStatus string

const (
	// VersioningEnabled 启用版本控制.
	VersioningEnabled BucketVersioningStatus = "Enabled"
	// VersioningSuspended 暂停版本控制.
	VersioningSuspended BucketVersioningStatus = "Suspended"
)
