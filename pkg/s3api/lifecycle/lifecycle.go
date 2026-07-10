package lifecycle

import (
	"encoding/xml"
	"fmt"
	"io"
)

const defaultXMLNS = "http://s3.amazonaws.com/doc/2006-03-01/"

// Config 是 bucket 生命周期配置.
type Config struct {
	XMLName xml.Name `xml:"LifecycleConfiguration"`
	XMLNS   string   `xml:"xmlns,attr,omitempty"`
	Rules   []Rule   `xml:"Rule"`
}

// Rule 单条生命周期规则.
type Rule struct {
	XMLName xml.Name `xml:"Rule"`
	ID      string   `xml:"ID,omitempty"`
	Status  string   `xml:"Status"` // Enabled / Disabled
	Filter  *Filter  `xml:"Filter,omitempty"`
	// 过渡规则: 何时转换存储类型
	Transitions []Transition `xml:"Transition,omitempty"`
	// 过期规则: 何时删除对象
	Expiration *Expiration `xml:"Expiration,omitempty"`
	// 非当前版本过期
	NoncurrentVersionExpiration *NoncurrentVersionExpiration `xml:"NoncurrentVersionExpiration,omitempty"`
	// 非当前版本过渡
	NoncurrentVersionTransitions []NoncurrentVersionTransition `xml:"NoncurrentVersionTransition,omitempty"`
	// 中止未完成的分片上传
	AbortIncompleteMultipartUpload *AbortIncompleteMultipartUpload `xml:"AbortIncompleteMultipartUpload,omitempty"`
}

// Filter 过滤规则.
type Filter struct {
	XMLName xml.Name `xml:"Filter"`
	Prefix  string   `xml:"Prefix,omitempty"`
	Tag     *Tag     `xml:"Tag,omitempty"`
	// And 用于组合多个条件
	And *And `xml:"And,omitempty"`
}

// Tag 标签过滤.
type Tag struct {
	XMLName xml.Name `xml:"Tag"`
	Key     string   `xml:"Key"`
	Value   string   `xml:"Value"`
}

// And 组合过滤条件.
type And struct {
	XMLName xml.Name `xml:"And"`
	Prefix  string   `xml:"Prefix,omitempty"`
	Tags    []Tag    `xml:"Tag,omitempty"`
}

// Transition 过渡规则.
type Transition struct {
	XMLName      xml.Name `xml:"Transition"`
	Days         *int     `xml:"Days,omitempty"`
	Date         string   `xml:"Date,omitempty"`
	StorageClass string   `xml:"StorageClass"`
}

// Expiration 过期规则.
type Expiration struct {
	XMLName xml.Name `xml:"Expiration"`
	Days    *int     `xml:"Days,omitempty"`
	Date    string   `xml:"Date,omitempty"`
	// ExpiredObjectDeleteMarker 仅对单版本对象有效.
	ExpiredObjectDeleteMarker *bool `xml:"ExpiredObjectDeleteMarker,omitempty"`
}

// NoncurrentVersionExpiration 非当前版本过期.
type NoncurrentVersionExpiration struct {
	XMLName        xml.Name `xml:"NoncurrentVersionExpiration"`
	NoncurrentDays *int     `xml:"NoncurrentDays,omitempty"`
}

// NoncurrentVersionTransition 非当前版本过渡.
type NoncurrentVersionTransition struct {
	XMLName        xml.Name `xml:"NoncurrentVersionTransition"`
	NoncurrentDays *int     `xml:"NoncurrentDays,omitempty"`
	StorageClass   string   `xml:"StorageClass"`
}

// AbortIncompleteMultipartUpload 中止未完成的分片上传.
type AbortIncompleteMultipartUpload struct {
	XMLName             xml.Name `xml:"AbortIncompleteMultipartUpload"`
	DaysAfterInitiation *int     `xml:"DaysAfterInitiation"`
}

// NewConfig 创建一个新的生命周期配置.
func NewConfig(rules []Rule) *Config {
	return &Config{
		XMLNS: defaultXMLNS,
		Rules: rules,
	}
}

// ParseBucketLifecycleConfig 从 XML 解析生命周期配置.
func ParseBucketLifecycleConfig(reader io.Reader) (*Config, error) {
	var c Config
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
func (c *Config) ToXML() ([]byte, error) {
	if c.XMLNS == "" {
		c.XMLNS = defaultXMLNS
	}
	data, err := xml.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("marshaling lifecycle xml: %w", err)
	}
	return append([]byte(xml.Header), data...), nil
}
