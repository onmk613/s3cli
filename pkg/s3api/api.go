package s3api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	endpointURL  *url.URL
	accessKey    string
	secretKey    string
	sessionToken string
	region       string
	vendor       Provider

	lookup   BucketLookupType
	lookupFn BucketLookupFunc

	transport  http.RoundTripper
	httpClient *http.Client

	maxRetries int
}

type Options struct {
	Endpoint       string
	AccessKey      string
	SecretKey      string
	SessionToken   string
	Region         string
	Vendor         Provider
	NotCheckVendor bool

	BucketLookup       BucketLookupType
	BucketLookupViaURL BucketLookupFunc

	Transport http.RoundTripper

	MaxRetries int
}

func New(opts *Options) (*Client, error) {
	if opts == nil {
		return nil, errors.New("options cannot be nil")
	}

	endpointURL, err := url.Parse(opts.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid endpoint URL: %v", err)
	}

	if opts.Endpoint == "" {
		return nil, errors.New("endpoint cannot be empty")
	}

	if opts.AccessKey == "" {
		return nil, errors.New("access key cannot be empty")
	}

	if opts.SecretKey == "" {
		return nil, errors.New("secret key cannot be empty")
	}

	if opts.BucketLookupViaURL == nil && opts.BucketLookup == BucketLookupAuto {
		opts.BucketLookup = BucketLookupPath
	}

	if opts.Region == "" {
		opts.Region = "us-east-1"
	}

	if opts.MaxRetries <= 0 {
		opts.MaxRetries = 3
	}

	transport := opts.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}

	if !opts.NotCheckVendor {
		opts.Vendor = detectVendor(context.Background(), opts.Endpoint, opts.AccessKey, opts.SecretKey, opts.SessionToken, opts.Region, transport)
	}

	return &Client{
		endpointURL:  endpointURL,
		accessKey:    opts.AccessKey,
		secretKey:    opts.SecretKey,
		sessionToken: opts.SessionToken,
		region:       opts.Region,
		vendor:       opts.Vendor,
		lookup:       opts.BucketLookup,
		lookupFn:     opts.BucketLookupViaURL,
		transport:    transport,
		httpClient:   &http.Client{Transport: transport},
		maxRetries:   opts.MaxRetries,
	}, nil
}

// Endpoint 返回配置的 endpoint 字符串.
func (c *Client) Endpoint() string {
	if c.endpointURL == nil {
		return ""
	}
	return c.endpointURL.String()
}

// Region 返回配置的区域.
func (c *Client) Region() string {
	return c.region
}

// AccessKey 返回配置的 AccessKey.
func (c *Client) AccessKey() string {
	return c.accessKey
}

// SecretKey 返回配置的 SecretKey.
func (c *Client) SecretKey() string {
	return c.secretKey
}

// SessionToken 返回配置的 SessionToken.
func (c *Client) SessionToken() string {
	return c.sessionToken
}

// Vendor 返回 S3 厂商类型 (aws / cos / oss / minio).
func (c *Client) Vendor() Provider {
	return c.vendor
}

type BucketLookupType int

const (
	BucketLookupAuto BucketLookupType = iota
	BucketLookupDNS
	BucketLookupPath
)

type BucketLookupFunc interface {
	ResolveCustomEndpoint(bucket string) (*url.URL, error)
}

// resolveURL 根据 bucket 寻址方式 (path / virtual-host / 自定义模板) 生成目标 URL.
func (c *Client) resolveURL(bucketName, objectName string, queryValues url.Values) (*url.URL, error) {
	// 自定义 endpoint 模板: 把 bucket 名替换进模板生成完整 URL
	if c.lookupFn != nil {
		customURL, err := c.lookupFn.ResolveCustomEndpoint(bucketName)
		if err != nil {
			return nil, err
		}
		return c.buildURL(customURL.Host, customURL.Scheme, bucketName, objectName, queryValues, BucketLookupDNS)
	}

	lookup := c.lookup
	if lookup == BucketLookupAuto {
		lookup = BucketLookupPath
	}
	return c.buildURL(c.endpointURL.Host, c.endpointURL.Scheme, bucketName, objectName, queryValues, lookup)
}

// buildURL 根据寻址方式拼接最终请求 URL.
// host/scheme 为基准地址; DNS 风格会把 bucket 前置到 host, path 风格把 bucket 放入路径.
func (c *Client) buildURL(host, scheme, bucketName, objectName string, queryValues url.Values, lookup BucketLookupType) (*url.URL, error) {
	var p string
	switch {
	case bucketName == "":
		// service 级请求, 如 ListBuckets
		p = "/"
	case lookup == BucketLookupDNS:
		host = bucketName + "." + host
		p = "/"
		if objectName != "" {
			p += encodePath(objectName)
		}
	default: // BucketLookupPath
		p = "/" + bucketName
		if objectName != "" {
			p += "/" + encodePath(objectName)
		}
		// objectName 为空时保持 "/test-bucket"，不加尾斜杠
	}

	u := &url.URL{
		Scheme:  scheme,
		Host:    host,
		Path:    p,
		RawPath: p,
	}
	if len(queryValues) > 0 {
		u.RawQuery = s3EncodeQuery(queryValues)
	}
	return u, nil
}

// requestMetadata 描述一次 S3 API 请求所需的全部元数据.
type requestMetadata struct {
	// 请求路由
	bucketName     string
	objectName     string
	bucketLocation string // 可选, 用于创建 bucket 时指定区域
	customPath     string // 可选, 用于指定自定义路径

	// 查询参数, 如 ?versioning / ?uploads / list-type=2 等
	queryValues url.Values

	// 自定义请求头, 如 x-amz-meta-* / Content-Type 等
	customHeader http.Header

	// 请求体; 若可 Seek 则重试时会自动回卷
	contentBody   io.Reader
	contentLength int64

	// 预计算的摘要 (可选)
	contentSHA256Hex string // x-amz-content-sha256; 为空时按规则自动填充
	contentMD5Base64 string // Content-MD5
}

// newRequest 构建一个已签名 (SigV4) 的 *http.Request.
func (c *Client) newRequest(ctx context.Context, method string, meta requestMetadata) (*http.Request, error) {
	if method == "" {
		method = http.MethodGet
	}

	targetURL, err := c.resolveURL(meta.bucketName, meta.objectName, meta.queryValues)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, targetURL.String(), meta.contentBody)
	if err != nil {
		return nil, err
	}

	// 自定义头
	for k, vv := range meta.customHeader {
		for _, v := range vv {
			req.Header.Set(k, v)
		}
	}

	// Content-Length
	if meta.contentLength > 0 {
		req.ContentLength = meta.contentLength
	}
	if meta.contentMD5Base64 != "" {
		req.Header.Set("Content-MD5", meta.contentMD5Base64)
	}

	if meta.customPath != "" {
		req.URL.Path = meta.customPath
		req.URL.RawPath = meta.customPath
	}

	// x-amz-content-sha256:
	//   - 调用方预计算优先
	//   - 无 body 时使用空串 SHA256
	//   - 有 body 时若可全部读入/Seek 则计算, 否则 UNSIGNED-PAYLOAD
	shaHex := meta.contentSHA256Hex
	if shaHex == "" {
		switch {
		case meta.contentBody == nil:
			shaHex = emptySHA256Hex
		default:
			if s, ok := meta.contentBody.(io.ReadSeeker); ok {
				shaHex, err = hashSHA256Seeker(s)
				if err != nil {
					return nil, fmt.Errorf("hash request body: %w", err)
				}
			} else {
				shaHex = unsignedPayload
			}
		}
	}
	req.Header.Set("X-Amz-Content-Sha256", shaHex)

	if c.sessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", c.sessionToken)
	}

	// SigV4 签名
	signV4(req, c.accessKey, c.secretKey, c.region, shaHex, time.Now().UTC())
	return req, nil
}

// Do 执行一次完整的 S3 API 调用: 构建请求 -> 签名 -> 发送 -> 重试 -> 错误解析.
// 返回的 *http.Response 由调用方负责关闭 Body.
func (c *Client) Do(ctx context.Context, method string, meta requestMetadata) (*http.Response, error) {
	// 若 body 可 Seek, 记录起点以便重试回卷
	var seeker io.Seeker
	var bodyStart int64
	if s, ok := meta.contentBody.(io.ReadSeeker); ok {
		if pos, err := s.Seek(0, io.SeekCurrent); err == nil {
			seeker = s
			bodyStart = pos
		}
	}

	attempts := c.maxRetries + 1
	var lastErr error

	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			// 回卷 body; 不可回卷则无法安全重试
			if seeker == nil && meta.contentBody != nil {
				break
			}
			if seeker != nil {
				if _, err := seeker.Seek(bodyStart, io.SeekStart); err != nil {
					break
				}
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(retryBackoff(attempt)):
			}
		}

		req, err := c.newRequest(ctx, method, meta)
		if err != nil {
			return nil, err
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue // 网络层错误, 重试
		}

		// 成功
		if resp.StatusCode < 400 {
			return resp, nil
		}

		// 解析 S3 XML 错误
		apiErr := parseErrorResponse(resp, meta.bucketName, meta.objectName)
		_ = resp.Body.Close()
		lastErr = apiErr

		if !isRetryable(resp.StatusCode, apiErr) {
			return nil, apiErr
		}
	}

	if lastErr == nil {
		lastErr = errors.New("request failed with no response")
	}
	return nil, lastErr
}

// retryBackoff 指数退避 + 抖动.
func retryBackoff(attempt int) time.Duration {
	const (
		base     = 200 * time.Millisecond
		retryCap = 10 * time.Second
	)
	d := base << uint(attempt)
	if d > retryCap {
		d = retryCap
	}
	// 加入 0~50% 抖动
	jitter := time.Duration(rand.Int63n(int64(d) / 2))
	return d/2 + jitter
}

// isRetryable 判断响应是否可重试.
func isRetryable(statusCode int, apiErr *ErrorResponse) bool {
	switch statusCode {
	case http.StatusTooManyRequests, // 429
		http.StatusInternalServerError, // 500
		http.StatusBadGateway,          // 502
		http.StatusServiceUnavailable,  // 503
		http.StatusGatewayTimeout:      // 504
		return true
	}
	if apiErr != nil {
		switch apiErr.Code {
		case "SlowDown", "RequestTimeout", "InternalError", "ServiceUnavailable":
			return true
		}
	}
	return false
}

// s3EncodeQuery 按 S3 (RFC 3986) 规则编码查询参数, 键按字典序排序.
func s3EncodeQuery(v url.Values) string {
	if len(v) == 0 {
		return ""
	}
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sortStrings(keys)

	var buf strings.Builder
	for _, k := range keys {
		for _, val := range v[k] {
			if buf.Len() > 0 {
				buf.WriteByte('&')
			}
			buf.WriteString(percentEncode(k))
			buf.WriteByte('=')
			buf.WriteString(percentEncode(val))
		}
	}
	return buf.String()
}
