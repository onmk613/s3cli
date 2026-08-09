package client

import (
	"crypto/tls"
	"net"
	"net/http"
	"s3cli/pkg/s3iface"
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

// newS3Client 构建自建的 api.Client.
// cfg 提供别名相关的静态配置；flags 提供进程级 CLI 开关（debug / User-Agent / 自定义 header /
// --host-base / --no-verify-ssl 覆盖）。
func newS3Client(cfg config.Static, flags config.Flags) (*api.Client, error) {
	cfg = applyGlobalOverrides(cfg, flags)

	transport := &http.Transport{
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: cfg.NoVerifySSL},
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
