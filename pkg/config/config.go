// Package config 管理 s3cli 的配置文件（INI 格式），支持多别名、自定义 bucket 寻址。
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

var ConfigPath string
var DefaultConfigPath = filepath.Join(os.Getenv("HOME"), ".s3cli")

var (
	G  = &Config{}
	mu sync.RWMutex // 保护 Config 并发读写
)

type Config struct {
	S     map[string]Static
	Debug bool // --debug
}

// ── 线程安全的 Config 访问器 ──────────────────────────────────────

// GetSection 安全地读取指定 alias 的配置，不存在返回 nil。
func GetSection(alias string) *Static {
	mu.RLock()
	defer mu.RUnlock()
	if s, ok := G.S[alias]; ok {
		cp := s
		return &cp
	}
	return nil
}

// IsDebug 安全地读取 debug 标志。
func IsDebug() bool {
	mu.RLock()
	defer mu.RUnlock()
	return G.Debug
}

// SetDebug 安全地设置 debug 标志（仅在启动阶段使用）。
func SetDebug(v bool) {
	mu.Lock()
	defer mu.Unlock()
	G.Debug = v
}

// SetSections 安全地设置全部 section map（仅 LoadConf 调用）。
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
func (s *Static) IsDebug() bool           { return IsDebug() }
func (s *Static) IsVerifySSL() bool       { return s.VerifySSL }
