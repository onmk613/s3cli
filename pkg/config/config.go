// Package config 管理 s3cli 的配置文件（INI 格式），支持多别名、自定义 bucket 寻址。
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Defaults 集中管理硬编码默认值，避免散落各处。
const (
	BucketLookupPath   = "path"
	BucketLookupDNS    = "dns"
	BucketLookupCustom = "custom"
	BucketPlaceholder  = "%(bucket)"
	DefaultRegion      = "us-east-1"
	DefaultConcurrency = 10
	DefaultPartSizeMB  = 15
	DefaultMimeType    = "binary/octet-stream"
)

var ConfPath string
var DefaultConfigPath = filepath.Join(os.Getenv("HOME"), ".s3cli")

var (
	G  = &Config{}
	mu sync.RWMutex
)

type Config struct {
	S               map[string]Static
	Debug           bool          // --debug
	Quiet           bool          // --quiet (关闭进度条, 输出纯文本)
	UserAgent       string        // --user-agent (覆盖整个 User-Agent)
	UserAgentSuffix string        // --user-agent-suffix (追加到 User-Agent 末尾)
	Headers         []string      // --header (自定义 HTTP header, 可重复, 格式 key:value)
	RequestTimeout  time.Duration // --request-timeout，0 表示不设置总请求期限
	OutputFormat    string        // --output: text / json
}

// SetSections 安全地设置全部 section map
func SetSections(m map[string]Static) {
	mu.Lock()
	defer mu.Unlock()
	G.S = m
}

type Static struct {
	AccessKey    string `ini:"access_key"`
	SecretKey    string `ini:"secret_key"`
	SessionToken string `ini:"session_token"`
	HostBase     string `ini:"host_base"`

	Region    string `ini:"region"`
	VerifySSL bool   `ini:"verify_ssl"`
	// path / dns / https://www.%(bucket).example.com
	BucketLookup string `ini:"bucket_lookup"`

	DefaultMimeType      string `ini:"default_mime_type"`
	MultipartChunkSizeMb int    `ini:"multipart_chunk_size_mb"`
	MaxRetries           int    `ini:"max_retries"`
	Vendor               string `ini:"vendor"` // S3 厂商: aws / cos / oss / minio; 为空时自动探测
}

func (c *Static) ResolveBucketLookup() (mode string, tpl string, err error) {
	raw := strings.TrimSpace(c.BucketLookup)

	if strings.Contains(raw, BucketPlaceholder) {
		return BucketLookupCustom, raw, nil
	}

	switch strings.ToLower(raw) {
	case "", "path", "path-style":
		return BucketLookupPath, "", nil
	case "dns", "virtual", "virtualhost", "vhost", "virtual-hosted-style", "subdomain":
		return BucketLookupDNS, "", nil
	}

	if validateCustomTemplate(raw, BucketPlaceholder) {
		return BucketLookupCustom, raw, nil
	}

	return "", "", fmt.Errorf("invalid bucket_lookup %s, expected path / dns / custom-template containing %%(bucket)", raw)
}

func (c *Static) GetRegion() string {
	if c.Region != "" {
		return strings.TrimSpace(c.Region)
	}
	return DefaultRegion
}

func (c *Static) GetAccessKey() string {
	return strings.TrimSpace(c.AccessKey)
}

func (c *Static) GetSecretKey() string {
	return strings.TrimSpace(c.SecretKey)
}

func (c *Static) GetSessionToken() string {
	return strings.TrimSpace(c.SessionToken)
}

func (c *Static) GetEndpoint() string {
	return strings.TrimSpace(c.HostBase)
}

func (c *Static) IsDebug() bool {
	return G.Debug
}

func (c *Static) IsVerifySSL() bool {
	return c.VerifySSL
}

func (c *Static) GetUserAgent() string {
	return strings.TrimSpace(G.UserAgent)
}

func (c *Static) GetUserAgentSuffix() string {
	return strings.TrimSpace(G.UserAgentSuffix)
}

func (c *Static) GetHeaders() []string {
	return G.Headers
}

func (c *Static) GetMaxRetries() int {
	if c.MaxRetries > 0 {
		return c.MaxRetries
	}
	return 3
}

// GetVendor 返回配置的 S3 厂商类型; 为空时交给 s3api 自动探测.
func (c *Static) GetVendor() string {
	return strings.TrimSpace(c.Vendor)
}
