package client

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"s3cli/internal/api"
	"s3cli/internal/config"
)

// newBackendClient 构造自建的 api.Client.
func newBackendClient(static config.Static, flags config.Flags) (api.S3Operations, error) {
	return newS3Client(static, flags)
}

// applyGlobalOverrides 把 CLI 全局 flag 覆盖到别名静态配置上：
// --host-base 非空时替换 host_base（自定义 bucket 模板会完全接管 host，
// 与覆盖语义冲突，故一并降级为 path 寻址）；--no-verify-ssl 与别名配置取或（任一开启即跳过校验）。
//
// --host-base 的合法性 (scheme/host) 由 newS3Client 在调用本函数前校验,
// 与 `alias set` 走同一套 ValidateEndpoint 口径 —— 此前覆盖值绕过校验,
// 裸主机名会被 api 层默认补 http://, 凭证明文外发且零告警。
func applyGlobalOverrides(cfg config.Static, flags config.Flags) config.Static {
	if flags.HostBase != "" {
		cfg.HostBase = flags.HostBase
		if mode, _, err := cfg.ResolveBucketLookup(); err == nil && mode == config.BucketLookupCustom {
			cfg.BucketLookup = ""
		}
	}
	if flags.NoVerifySSL {
		cfg.NoVerifySSL = true
	}
	return cfg
}

// warnHostBaseTemplateConflict --host-base 与自定义桶模板冲突时, 模板会被
// applyGlobalOverrides 静默降级为 path 寻址; 提前向 stderr 输出警告, 避免用户无感知。
func warnHostBaseTemplateConflict(cfg config.Static, flags config.Flags) {
	if flags.HostBase == "" {
		return
	}
	if mode, _, err := cfg.ResolveBucketLookup(); err == nil && mode == config.BucketLookupCustom {
		fmt.Fprintf(os.Stderr, "warning: --host-base overrides the endpoint host; custom bucket_lookup template is ignored, falling back to path-style addressing\n")
	}
}

// tlsMinVersion 解析别名配置 tls_min_version, 返回 crypto/tls 常量。
// 缺省 (空串) 为 1.2; 老式自建 S3 端点可能只支持 1.0/1.1, 可显式放宽。
func tlsMinVersion(v string) (uint16, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "1.2", "tls1.2":
		return tls.VersionTLS12, nil
	case "1.0", "tls1.0":
		return tls.VersionTLS10, nil
	case "1.1", "tls1.1":
		return tls.VersionTLS11, nil
	case "1.3", "tls1.3":
		return tls.VersionTLS13, nil
	default:
		return 0, fmt.Errorf("invalid tls_min_version %q (expected 1.0 / 1.1 / 1.2 / 1.3)", v)
	}
}

// newS3Client 构建自建的 api.Client.
// cfg 提供别名相关的静态配置；flags 提供进程级 CLI 开关（debug / User-Agent / 自定义 header /
// --host-base / --no-verify-ssl 覆盖）。
func newS3Client(cfg config.Static, flags config.Flags) (*api.Client, error) {
	// --host-base 覆盖值必须先过 endpoint 校验 (scheme/host 完整性, http 告警),
	// 与 `alias set` 的入口校验同口径; 不能让全局覆盖成为绕过校验的旁路。
	if flags.HostBase != "" {
		if _, err := config.ValidateEndpoint(strings.TrimSpace(flags.HostBase)); err != nil {
			return nil, err
		}
	}
	warnHostBaseTemplateConflict(cfg, flags)
	cfg = applyGlobalOverrides(cfg, flags)

	minTLS, err := tlsMinVersion(cfg.TLSMinVersion)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{
		// 代理必须显式从环境变量读取: 自建 Transport 的 Proxy 零值是 nil (直连),
		// 会静默丢掉 http.DefaultTransport 的 ProxyFromEnvironment, 使
		// HTTP_PROXY / HTTPS_PROXY / NO_PROXY 全部失效。
		Proxy: http.ProxyFromEnvironment,
		// 关闭透明压缩: 与本包 api 层写在请求上的 Accept-Encoding: identity 双保险。
		// S3 对象可合法带 Content-Encoding: gzip, 而 Go 在自行请求 gzip 时会透明
		// 解压响应体并抹掉该头/Content-Length —— 那会让下载到的字节与桶里不一致。
		DisableCompression:    true,
		TLSClientConfig:       &tls.Config{MinVersion: minTLS, InsecureSkipVerify: cfg.NoVerifySSL},
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          100,
		// 与默认并发数解耦并留出余量: 取 10 (== defaultConcurrency) 时,
		// --concurrency 一调高就会因空闲连接耗尽而反复重新建连。
		MaxIdleConnsPerHost: 32,
	}
	var rt http.RoundTripper = transport

	// dump HTTP
	if flags.Debug {
		rt = newDumper(rt)
	}

	// User-Agent 改写放在最外层: 先改写请求, 再交给(可能存在的)tracer dump,
	// 这样 --debug 输出里看到的就是改写后的最终 User-Agent。
	if len(flags.UserAgent) > 0 || len(flags.UserAgentSuffix) > 0 {
		rt = newUserAgentTransport(rt, flags.UserAgent, flags.UserAgentSuffix)
	}

	// 自定义 HTTP header 不走 RoundTripper: 它们必须与签名时的头集合一致,
	// 因此由 api 层在签名前注入 (见 api.Options.ExtraHeaders)。
	extraHeaders, err := parseCustomHeaders(flags.Headers)
	if err != nil {
		return nil, err
	}

	lookup, customTpl, err := cfg.ResolveBucketLookup()
	if err != nil {
		return nil, err
	}

	var bucketLookup api.BucketLookupType
	var lookupFn api.BucketLookupFunc
	switch lookup {
	case config.BucketLookupPath:
		bucketLookup = api.BucketLookupPath
	case config.BucketLookupDNS:
		bucketLookup = api.BucketLookupDNS
	case config.BucketLookupCustom:
		bucketLookup = api.BucketLookupAuto
		lookupFn = &CustomBucketLookup{
			Template:          customTpl,
			BucketPlaceholder: config.BucketPlaceholder,
			RegionPlaceholder: config.RegionPlaceholder,
		}
	}

	opts := &api.Options{
		Endpoint:           cfg.HostBase,
		AccessKey:          cfg.AccessKey,
		SecretKey:          cfg.SecretKey,
		SessionToken:       cfg.SessionToken,
		Region:             cfg.Region,
		BucketLookup:       bucketLookup,
		BucketLookupViaURL: lookupFn,
		Transport:          rt,
		ExtraHeaders:       extraHeaders,
		MaxRetries:         cfg.MaxRetries,
		// 厂商差异开关: 别名配置直通 api 层 (零值 = 严格 AWS 语义)。
		Quirks: &api.Quirks{
			LifecycleRootElement:      cfg.LifecycleRootElement,
			LambdaNotificationElement: cfg.LambdaNotificationElement,
			DisableRegionRedirect:     cfg.DisableRegionRedirect,
			DisableRegionProbe:        cfg.DisableRegionProbe,
			ForceUnsignedPayload:      cfg.ForceUnsignedPayload,
			XMLNS:                     cfg.XMLNS,
		},
	}

	return api.New(opts)
}
