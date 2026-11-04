package api

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// CorsConfig 是 bucket 的 CORS 配置容器.
type CorsConfig struct {
	XMLName   xml.Name   `xml:"CORSConfiguration" json:"-"`
	XMLNS     string     `xml:"xmlns,attr,omitempty" json:"-"`
	CORSRules []CorsRule `xml:"CORSRule" json:"CORSRules,omitempty"`
}

// CorsRule 是单条 CORS 规则.
//
// 列表字段在 XML 中是单数元素名 (<AllowedOrigin> 逐个出现), 在 AWS CLI / SDK JSON 中是
// 复数键 (AllowedOrigins), 故两种 tag 名不同; JSON 侧沿用 AWS 命名以便
// `aws s3api get-bucket-cors` 的输出可直接使用。
type CorsRule struct {
	AllowedHeader []string `xml:"AllowedHeader,omitempty" json:"AllowedHeaders,omitempty"`
	AllowedMethod []string `xml:"AllowedMethod,omitempty" json:"AllowedMethods,omitempty"`
	AllowedOrigin []string `xml:"AllowedOrigin,omitempty" json:"AllowedOrigins,omitempty"`
	ExposeHeader  []string `xml:"ExposeHeader,omitempty" json:"ExposeHeaders,omitempty"`
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
