package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// userHomeDir 可注入钩子（测试用），避免直接依赖系统用户目录。
var userHomeDir = os.UserHomeDir

// DefaultConfigPath 返回默认配置文件路径 ~/.s3cli。
// pflag 注册时会把默认值写入 G.C，以便在未指定 --config 时使用。
func DefaultConfigPath() string {
	if home, err := userHomeDir(); err == nil {
		return filepath.Join(home, ".s3cli")
	}
	return ""
}

// 默认值与 bucket 寻址相关的合法取值，集中管理避免散落各处。
const (
	DefaultConcurrency = 10
	DefaultPartSizeMB  = 15
	BucketLookupPath   = "path"
	BucketLookupDNS    = "dns"
	BucketLookupCustom = "custom"
	BucketPlaceholder  = "%(bucket)"
	RegionPlaceholder  = "%(region)"

	// DefaultTLSMinVersion 是别名 TLS 最低版本的默认值 (与 Go 默认一致)。
	DefaultTLSMinVersion = "1.2"

	// MaxPartSizeMB 是 multipart_chunk_size_mb 的上界 (1GiB)。
	// 与 action.MaxPartSizeMB 同值: config 不依赖 action (保持分层), 故各自持有
	// 常量, 由两侧的边界用例共同锁定, 避免任一侧被悄悄改宽。
	MaxPartSizeMB = 1024
)

// G 是进程级运行时配置：别名表 + CLI 全局开关。
var G = &Config{}

// Config 持有别名配置表 (S) 与全局 CLI 开关 (Flags)。
type Config struct {
	S map[string]Static
	F Flags
	C string
}

// Flags 承载来自命令行全局 flag 的运行时开关，与单个别名无关。
// 由 cmd 层的 cobra flag 绑定（&config.G.Flags.X）写入。
type Flags struct {
	Debug           bool     // --debug 输出http请求摘要
	NoColor         bool     // --no-color 关闭彩色输出
	UserAgent       string   // --user-agent 覆盖整个 User-Agent
	UserAgentSuffix string   // --user-agent-suffix 追加到 User-Agent 末尾
	Headers         []string // --header 自定义 HTTP header, 可重复, 格式 key:value
	ShowSecret      bool     // --show-secret alias list 显示完整明文密钥 (默认脱敏)
	HostBase        string   // --host-base 覆盖所有别名的 endpoint host
	NoVerifySSL     bool     // --no-verify-ssl 全局跳过 TLS 证书校验 (与别名配置取或)
	JSON            bool     // --json 全局结构化输出开关 (支持的命令据此切换输出格式)
}

// Static 描述单个别名（一个 S3 端点）的静态配置。
// TOML tag 与磁盘文件 key 一一对应；与 INI 版本字段名基本一致，便于老用户对照迁移
// （注意 verify_ssl 已改为语义相反的 no_verify_ssl，迁移时需翻转）。
type Static struct {
	AccessKey    string `toml:"access_key"`
	SecretKey    string `toml:"secret_key"`
	SessionToken string `toml:"session_token"`
	HostBase     string `toml:"host_base"`

	Region      string `toml:"region"`
	NoVerifySSL bool   `toml:"no_verify_ssl"`
	// path / dns / https://www.%(bucket).example.com
	BucketLookup string `toml:"bucket_lookup"`

	DefaultMimeType      string `toml:"default_mime_type"`
	MultipartChunkSizeMb int    `toml:"multipart_chunk_size_mb"`
	MaxRetries           int    `toml:"max_retries"`

	// TLS 最低版本: 1.0 / 1.1 / 1.2 / 1.3, 缺省 1.2。
	// 老式自建 S3 端点可能只支持 1.0/1.1, 可显式放宽 (no_verify_ssl 不降低协议版本)。
	TLSMinVersion string `toml:"tls_min_version"`

	// ---- 厂商差异开关 (见 pkg/api/quirks.go) ----
	// 缺省 (零值) 即严格 AWS 语义; 仅在对接特定 S3 兼容实现时按需设置,
	// 目的是让适配新厂商停留在配置层, 不必改代码。

	// LifecycleRootElement 覆盖 PUT ?lifecycle 的根元素名 (缺省 LifecycleConfiguration)。
	LifecycleRootElement string `toml:"lifecycle_root_element"`
	// LambdaNotificationElement 选择 Lambda 通知元素命名: cloud (缺省) / lambda。
	LambdaNotificationElement string `toml:"lambda_notification_element"`
	// DisableRegionRedirect 关闭 301/307/400 + X-Amz-Bucket-Region 的重签重发。
	DisableRegionRedirect bool `toml:"disable_region_redirect"`
	// DisableRegionProbe 关闭自定义寻址模板 %(region) 的 GetBucketLocation 探测。
	DisableRegionProbe bool `toml:"disable_region_probe"`
	// ForceUnsignedPayload 强制 x-amz-content-sha256: UNSIGNED-PAYLOAD。
	ForceUnsignedPayload bool `toml:"force_unsigned_payload"`
	// XMLNS 覆盖 CORS / 生命周期 XML 的命名空间 (缺省 S3 标准命名空间)。
	XMLNS string `toml:"xmlns"`
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
