package client

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"time"

	"s3cli/internal/config"
	"s3cli/pkg/s3api"
)

// NewS3Client 构建自建的 s3api.Client.
// cfg 提供别名相关的静态配置；flags 提供进程级 CLI 开关（debug / User-Agent / 自定义 header）。
func NewS3Client(_ context.Context, cfg config.Static, flags config.Flags) (*s3api.Client, error) {
	transport := &http.Transport{
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: !cfg.VerifySSL},
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
	}
	var rt http.RoundTripper = transport

	if flags.Debug {
		rt = NewDumper(rt)
	}

	// User-Agent 改写放在最外层: 先改写请求, 再交给(可能存在的)tracer dump,
	// 这样 --debug 输出里看到的就是改写后的最终 User-Agent。
	rt = newUserAgentTransport(rt, flags.UserAgent, flags.UserAgentSuffix)

	// 自定义 HTTP header 注入(放在最外层, --debug 可见最终请求头)。
	customHeaders, err := parseHeaders(flags.Headers)
	if err != nil {
		return nil, err
	}
	rt = newCustomHeaderTransport(rt, customHeaders)

	lookup, customTpl, err := cfg.ResolveBucketLookup()
	if err != nil {
		return nil, err
	}

	var bucketLookup s3api.BucketLookupType
	var lookupFn s3api.BucketLookupFunc
	switch lookup {
	case config.BucketLookupPath:
		bucketLookup = s3api.BucketLookupPath
	case config.BucketLookupDNS:
		bucketLookup = s3api.BucketLookupDNS
	case config.BucketLookupCustom:
		bucketLookup = s3api.BucketLookupAuto
		lookupFn = &CustomBucketLookup{
			Template:          customTpl,
			BucketPlaceholder: config.BucketPlaceholder,
			RegionPlaceholder: config.RegionPlaceholder,
		}
	}

	opts := &s3api.Options{
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

	return s3api.New(opts)
}
