package client

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"time"

	"s3cli/pkg/config"
	"s3cli/pkg/s3api"
)

// NewS3Client 构建自建的 s3api.Client.
func NewS3Client(_ context.Context, cfg config.Static) (*s3api.Client, error) {
	transport := &http.Transport{
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: !cfg.IsVerifySSL()},
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
	}
	var rt http.RoundTripper = transport

	if cfg.IsDebug() {
		rt = NewDumper(rt)
	}

	// User-Agent 改写放在最外层: 先改写请求, 再交给(可能存在的)tracer dump,
	// 这样 --debug 输出里看到的就是改写后的最终 User-Agent。
	rt = newUserAgentTransport(rt, cfg.GetUserAgent(), cfg.GetUserAgentSuffix())

	// 自定义 HTTP header 注入(放在最外层, --debug 可见最终请求头)。
	customHeaders, err := parseHeaders(cfg.GetHeaders())
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
		lookupFn = &CustomBucketLookup{Template: customTpl, BucketPlaceholder: config.BucketPlaceholder}
	}

	var notCheckVendor bool
	var s3apiProvider s3api.Provider
	if cfg.GetVendor() != "" {
		notCheckVendor = true
		s3apiProvider = s3api.Provider(cfg.GetVendor())
	}

	opts := &s3api.Options{
		Endpoint:           cfg.GetEndpoint(),
		AccessKey:          cfg.GetAccessKey(),
		SecretKey:          cfg.GetSecretKey(),
		SessionToken:       cfg.GetSessionToken(),
		Region:             cfg.GetRegion(),
		Vendor:             s3apiProvider,
		NotCheckVendor:     notCheckVendor,
		BucketLookup:       bucketLookup,
		BucketLookupViaURL: lookupFn,
		Transport:          rt,
		MaxRetries:         cfg.GetMaxRetries(),
	}

	return s3api.New(opts)
}
