package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
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
	ShowSecret      bool     // --show-secret alias list 显示完整明文密钥 (默认脱敏)
}

// Static 描述单个别名（一个 S3 端点）的静态配置。
// TOML tag 与磁盘文件 key 一一对应；与 INI 版本字段名完全一致，便于老用户对照迁移。
type Static struct {
	AccessKey    string `toml:"access_key"`
	SecretKey    string `toml:"secret_key"`
	SessionToken string `toml:"session_token"`
	HostBase     string `toml:"host_base"`

	Region    string `toml:"region"`
	VerifySSL bool   `toml:"verify_ssl"`
	// path / dns / https://www.%(bucket).example.com
	BucketLookup string `toml:"bucket_lookup"`

	DefaultMimeType      string `toml:"default_mime_type"`
	MultipartChunkSizeMb int    `toml:"multipart_chunk_size_mb"`
	MaxRetries           int    `toml:"max_retries"`
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

// buildOutputMap 把单个别名转换为 map[string]any，跳过取默认值的字段，
// 让写入的配置文件保持简洁（与原 INI 版本里 DeleteKey 默认值字段的语义一致）。
// 字段裁剪规则：
//   - access_key / secret_key / host_base 必写
//   - session_token / region / bucket_lookup / default_mime_type: 非空才写
//   - verify_ssl: 仅 false 才写 (缺省 = true，由 LoadConf 在读取时回填)
//   - multipart_chunk_size_mb: > 0 且 != 15 才写
//   - max_retries: > 0 才写
func buildOutputMap(s Static) map[string]any {
	m := map[string]any{
		"access_key": s.AccessKey,
		"secret_key": s.SecretKey,
		"host_base":  s.HostBase,
	}
	if s.SessionToken != "" {
		m["session_token"] = s.SessionToken
	}
	if s.Region != "" {
		m["region"] = s.Region
	}
	if !s.VerifySSL {
		m["verify_ssl"] = false
	}
	if s.BucketLookup != "" {
		m["bucket_lookup"] = s.BucketLookup
	}
	if s.DefaultMimeType != "" {
		m["default_mime_type"] = s.DefaultMimeType
	}
	if s.MultipartChunkSizeMb > 0 && s.MultipartChunkSizeMb != DefaultPartSizeMB {
		m["multipart_chunk_size_mb"] = s.MultipartChunkSizeMb
	}
	if s.MaxRetries > 0 {
		m["max_retries"] = s.MaxRetries
	}
	return m
}

// saveConfig 以原子方式写入凭据，并设置仅限所有者访问的权限。
// 输入是 alias 名 → Static 的整张表；内部按 buildOutputMap 过滤后用 TOML 编码。
func saveConfig(aliases map[string]Static, filename string) error {
	out := make(map[string]map[string]any, len(aliases))
	for name, s := range aliases {
		out[name] = buildOutputMap(s)
	}

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
	if err := toml.NewEncoder(tmp).Encode(out); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("encode config: %w", err)
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
