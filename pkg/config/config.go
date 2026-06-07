package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	BucketLookupPath   = "path"
	BucketLookupDNS    = "dns"
	BucketLookupCustom = "custom"
	BucketPlaceholder  = "%(bucket)"
)

var ConfigPath string
var DefaultConfigPath = filepath.Join(os.Getenv("HOME"), ".s3cli")

var G = &Config{}

type Config struct {
	S     map[string]Static
	Debug bool // --debug
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

	return "", "", fmt.Errorf("Invalid bucket_lookup %s, expected path / dns / custom-template containing %%(bucket)", raw)
}

// DefaultRegion 当用户未配置 region 时返回的默认值
const DefaultRegion = "us-east-1"

func (c *Static) GetRegion() string {
	if c.Region != "" {
		return strings.TrimSpace(c.Region)
	}
	return DefaultRegion
}

func (s *Static) GetAccessKey() string    { return strings.TrimSpace(s.AccessKey) }
func (s *Static) GetSecretKey() string    { return strings.TrimSpace(s.SecretKey) }
func (s *Static) GetSessionToken() string { return strings.TrimSpace(s.SessionToken) }
func (s *Static) GetEndpoint() string     { return strings.TrimSpace(s.HostBase) }
func (s *Static) IsDebug() bool           { return G.Debug }
func (s *Static) IsVerifySSL() bool       { return s.VerifySSL }
