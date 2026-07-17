package s3api

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// SetBucketCors 设置指定 bucket 的 CORS 配置.
func (c *Client) SetBucketCors(ctx context.Context, bucketName string, config *CorsConfig) error {
	body, err := config.toXML()
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

// CorsConfig is the container for a CORS configuration for a bucket.
// 线协议为 XML；JSON tag 用于本地配置文件读写与展示。
// XMLName/XMLNS 仅服务于 XML，对 JSON 标记为 "-"，避免污染输出。
type CorsConfig struct {
	XMLName   xml.Name   `xml:"CORSConfiguration" json:"-"`
	XMLNS     string     `xml:"xmlns,attr,omitempty" json:"-"`
	CORSRules []CorsRule `xml:"CORSRule" json:"CORSRules,omitempty"`
}

// CorsRule is a single rule in a CORS configuration.
type CorsRule struct {
	AllowedHeader []string `xml:"AllowedHeader,omitempty" json:"AllowedHeader,omitempty"`
	AllowedMethod []string `xml:"AllowedMethod,omitempty" json:"AllowedMethod,omitempty"`
	AllowedOrigin []string `xml:"AllowedOrigin,omitempty" json:"AllowedOrigin,omitempty"`
	ExposeHeader  []string `xml:"ExposeHeader,omitempty" json:"ExposeHeader,omitempty"`
	ID            string   `xml:"ID,omitempty" json:"ID,omitempty"`
	MaxAgeSeconds int      `xml:"MaxAgeSeconds,omitempty" json:"MaxAgeSeconds,omitempty"`
}

// ParseBucketCorsConfig parses a CORS configuration in XML from an io.Reader.
func ParseBucketCorsConfig(reader io.Reader) (*CorsConfig, error) {
	var c CorsConfig

	err := xml.NewDecoder(io.LimitReader(reader, 128*1024)).Decode(&c)
	if err != nil {
		return nil, fmt.Errorf("decoding xml: %w", err)
	}
	if c.XMLNS == "" {
		c.XMLNS = defaultXMLNS
	}
	for i, rule := range c.CORSRules {
		for j, method := range rule.AllowedMethod {
			c.CORSRules[i].AllowedMethod[j] = strings.ToUpper(method)
		}
	}
	return &c, nil
}

// toXML marshals the CORS configuration to XML.
func (c *CorsConfig) toXML() ([]byte, error) {
	if c.XMLNS == "" {
		c.XMLNS = defaultXMLNS
	}
	data, err := xml.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("marshaling xml: %w", err)
	}
	return append([]byte(xml.Header), data...), nil
}
