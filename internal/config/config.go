package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/ini.v1"
)

// Defaults 集中管理硬编码默认值，避免散落各处。
const (
	BucketLookupPath   = "path"
	BucketLookupDNS    = "dns"
	BucketLookupCustom = "custom"
	BucketPlaceholder  = "%(bucket)"
	RegionPlaceholder  = "%(region)"
	DefaultConcurrency = 10
	DefaultPartSizeMB  = 15
	DefaultMimeType    = "binary/octet-stream"
)

// ConfPath 配置文件路径
var ConfPath string

// G 是进程级运行时配置：别名表 + CLI 全局开关。
var G = &Config{}

// Config 持有别名配置表 (S) 与全局 CLI 开关 (Flags)。
type Config struct {
	S map[string]Static
	F Flags
}

// Flags 承载来自命令行全局 flag 的运行时开关，与单个别名无关。
// 由 pkg/cmd 层的 cobra flag 绑定（&config.G.Flags.X）写入。
type Flags struct {
	Debug           bool     // --debug 输出http请求摘要
	NoColor         bool     // --no-color 关闭彩色输出
	Quiet           bool     // --quiet 关闭进度条, 输出纯文本
	UserAgent       string   // --user-agent 覆盖整个 User-Agent
	UserAgentSuffix string   // --user-agent-suffix 追加到 User-Agent 末尾
	Headers         []string // --header 自定义 HTTP header, 可重复, 格式 key:value
	OutputJson      bool     // --json json格式输出, 针对部分操作有效
}

// Static 描述单个别名（一个 S3 端点）的静态配置。
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
	// 指定厂商, 详情请看 pkg/s3api/vender.go
	// 当前只为预留隐藏参数，还没正式做调整
	Vendor string `ini:"vendor"`
}

// ResolveBucketLookup 解析 bucket_lookup 配置，返回模式和模板。
func (c *Static) ResolveBucketLookup() (mode string, tpl string, err error) {
	// 优先判断 path / dns, 默认path
	switch strings.ToLower(c.BucketLookup) {
	case "", "path":
		return BucketLookupPath, "", nil
	case "dns":
		return BucketLookupDNS, "", nil
	}

	// 判断是否符合自定义模板规范
	if validateCustomTemplate(c.BucketLookup) {
		return BucketLookupCustom, c.BucketLookup, nil
	}

	return "", "", fmt.Errorf("invalid bucket_lookup %s, expected path / dns / custom-template containing %%(bucket)", c.BucketLookup)
}

// ensureConfPath 保证 ConfPath 非空，若为空则使用默认路径 ~/.s3cli
func ensureConfPath() string {
	if ConfPath == "" {
		ConfPath = filepath.Join(os.Getenv("HOME"), ".s3cli")
	}
	return ConfPath
}

// validateCustomTemplate 自定义寻址模板的合法性检查。
// %(bucket) 必须存在; %(region) 可选; 两者替换为测试值后结果须为合法 URL。
func validateCustomTemplate(tpl string) bool {
	// %(bucket) 必须包含、不能在最末尾、仅一次
	if !strings.Contains(tpl, BucketPlaceholder) {
		return false
	}
	if strings.HasSuffix(tpl, BucketPlaceholder) {
		return false
	}
	if strings.Count(tpl, BucketPlaceholder) > 1 {
		return false
	}

	// %(region) 可选, 但最多出现一次
	if strings.Count(tpl, RegionPlaceholder) > 1 {
		return false
	}

	// 用测试值替换两个占位符，验证结果是否为合法 URL
	testURL := strings.ReplaceAll(tpl, BucketPlaceholder, "test-bucket")
	testURL = strings.ReplaceAll(testURL, RegionPlaceholder, "us-east-1")
	u, err := url.Parse(testURL)
	if err != nil {
		return false
	}

	//  host 不能为空
	if u.Host == "" {
		return false
	}

	// host 中不能有连续的点（如 ..example.com）
	if strings.Contains(u.Host, "..") {
		return false
	}

	return true
}

// saveConfig 以原子方式写入凭据，并设置仅限所有者访问的权限
func saveConfig(cfg *ini.File, filename string) error {
	dir := filepath.Dir(filename)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".s3cli-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := cfg.WriteTo(tmp); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, filename); err != nil {
		return err
	}
	return os.Chmod(filename, 0o600)
}
