package cors

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

const defaultXMLNS = "http://s3.amazonaws.com/doc/2006-03-01/"

// Config is the container for a CORS configuration for a bucket.
type Config struct {
	XMLNS     string     `xml:"xmlns,attr,omitempty"`
	XMLName   xml.Name   `xml:"CORSConfiguration"`
	CORSRules []CORSRule `xml:"CORSRule"`
}

// Rule is a single rule in a CORS configuration.
type CORSRule struct {
	AllowedHeader []string `xml:"AllowedHeader,omitempty"`
	AllowedMethod []string `xml:"AllowedMethod,omitempty"`
	AllowedOrigin []string `xml:"AllowedOrigin,omitempty"`
	ExposeHeader  []string `xml:"ExposeHeader,omitempty"`
	ID            string   `xml:"ID,omitempty"`
	MaxAgeSeconds int      `xml:"MaxAgeSeconds,omitempty"`
}

// NewConfig creates a new CORS configuration with the given rules.
func NewConfig(rules []CORSRule) *Config {
	return &Config{
		XMLNS: defaultXMLNS,
		XMLName: xml.Name{
			Local: "CORSConfiguration",
			Space: defaultXMLNS,
		},
		CORSRules: rules,
	}
}

// ParseBucketCorsConfig parses a CORS configuration in XML from an io.Reader.
func ParseBucketCorsConfig(reader io.Reader) (*Config, error) {
	var c Config

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

// ToXML marshals the CORS configuration to XML.
func (c Config) ToXML() ([]byte, error) {
	if c.XMLNS == "" {
		c.XMLNS = defaultXMLNS
	}
	data, err := xml.Marshal(&c)
	if err != nil {
		return nil, fmt.Errorf("marshaling xml: %w", err)
	}
	return append([]byte(xml.Header), data...), nil
}
