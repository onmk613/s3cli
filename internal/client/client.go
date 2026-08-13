package client

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"s3cli/pkg/s3iface"
	"strings"
	"time"

	"s3cli/internal/config"
	"s3cli/pkg/api"
)

// newBackendClient 构造自建的 api.Client.
func newBackendClient(static config.Static, flags config.Flags) (s3iface.S3Operations, error) {
	return newS3Client(static, flags)
}

// applyGlobalOverrides 把 CLI 全局 flag 覆盖到别名静态配置上：
// --host-base 非空时替换 host_base（自定义 bucket 模板会完全接管 host，
// 与覆盖语义冲突，故一并降级为 path 寻址）；--no-verify-ssl 与别名配置取或（任一开启即跳过校验）。
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
	warnHostBaseTemplateConflict(cfg, flags)
	cfg = applyGlobalOverrides(cfg, flags)

	minTLS, err := tlsMinVersion(cfg.TLSMinVersion)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{
		TLSClientConfig:       &tls.Config{MinVersion: minTLS, InsecureSkipVerify: cfg.NoVerifySSL},
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
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

	// 自定义 HTTP header 注入(放在最外层, --debug 可见最终请求头)。
	if len(flags.Headers) > 0 {
		var err error
		rt, err = newHeaderTransport(rt, flags.Headers)
		if err != nil {
			return nil, err
		}
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
		MaxRetries:         cfg.MaxRetries,
	}

	return api.New(opts)
}
