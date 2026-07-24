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

	"s3cli/pkg/kvcache"
)

type Client struct {
	// 基础配置
	endpointURL  *url.URL
	accessKey    string
	secretKey    string
	sessionToken string

	// 地区和厂商
	region string

	// bucket 寻址方式和自定义寻址函数
	lookup   BucketLookupType
	lookupFn BucketLookupFunc

	// bucketLocCache 缓存 bucket->region, 供含 %(region) 的自定义寻址模板复用,
	// 避免每次请求都探测 GetBucketLocation。
	bucketLocCache *kvcache.Cache[string, string]

	// httpClient 用于发送 HTTP 请求, 可自定义 Transport.
	httpClient *http.Client

	// 最大重试次数
	maxRetries int
}

type Options struct {
	// 基础配置
	Endpoint     string
	AccessKey    string
	SecretKey    string
	SessionToken string

	// 地区和厂商
	Region string

	// bucket 寻址方式和自定义寻址函数
	BucketLookup       BucketLookupType
	BucketLookupViaURL BucketLookupFunc

	// 自定义http.Transport, 用于注入自定义header / 代理 / 证书等.
	Transport http.RoundTripper

	// 最大重试次数
	MaxRetries int
}

func New(opts *Options) (*Client, error) {
	// 检查必填参数
	if opts == nil || opts.Endpoint == "" || opts.AccessKey == "" || opts.SecretKey == "" {
		return nil, errors.New("endpoint, access key, and secret key cannot be empty")
	}

	// 补齐http头部
	if !strings.Contains(opts.Endpoint, "://") {
		opts.Endpoint = "http://" + opts.Endpoint
	}

	// 解析endpoint为url.URL
	endpointURL, err := url.Parse(opts.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid endpoint URL: %v", err)
	}

	// 解析寻址函数
	if opts.BucketLookupViaURL == nil && opts.BucketLookup == BucketLookupAuto {
		opts.BucketLookup = BucketLookupPath
	}

	// region
	if opts.Region == "" {
		opts.Region = "us-east-1"
	}

	// 重试次数
	if opts.MaxRetries <= 0 {
		opts.MaxRetries = 3
	}

	// transport
	transport := opts.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}

	return &Client{
		endpointURL:    endpointURL,
		accessKey:      opts.AccessKey,
		secretKey:      opts.SecretKey,
		sessionToken:   opts.SessionToken,
		region:         opts.Region,
		lookup:         opts.BucketLookup,
		lookupFn:       opts.BucketLookupViaURL,
		bucketLocCache: &kvcache.Cache[string, string]{},
		httpClient:     &http.Client{Transport: transport},
		maxRetries:     opts.MaxRetries,
	}, nil
}

// Endpoint 返回配置的 endpoint 字符串.
func (c *Client) Endpoint() string {
	return c.endpointURL.String()
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

// requestMetadata 描述一次 S3 API 请求所需的全部元数据.
type requestMetadata struct {
	// 请求路由
	bucketName     string
	objectName     string
	bucketLocation string // 可选, 用于创建 bucket 时指定区域

	// 可选, 用于指定自定义路径
	customPath string
	// forcePathStyle 强制用基准 endpoint 的 path-style 寻址, 跳过自定义模板与 region 解析。
	// 用于 GetBucketLocation 探测, 打破自定义 region 寻址的引导环。
	forcePathStyle bool
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
func (c *Client) newRequest(ctx context.Context, method string, meta requestMetadata, signingRegion string) (*http.Request, error) {
	if method == "" {
		method = http.MethodPost
	}

	// forcePathStyle: 跳过自定义寻址与 region 解析, 直接用基准 endpoint 的 path-style。
	// 用于 GetBucketLocation 探测, 打破 "需要 region 才能寻址, 寻址才能拿 region" 的引导环。
	var err error
	var targetURL *url.URL
	if meta.forcePathStyle {
		targetURL, err = c.buildURL(c.endpointURL.Host, c.endpointURL.Scheme, meta.bucketName, meta.objectName, meta.queryValues, BucketLookupPath)
	} else {
		targetURL, err = c.resolveURL(ctx, meta.bucketName, meta.objectName, meta.queryValues)
	}
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
	if signingRegion == "" {
		signingRegion = c.region
	}
	signV4(req, c.accessKey, c.secretKey, signingRegion, shaHex, time.Now().UTC())
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
	signingRegion := c.region
	redirects := 0

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

		req, err := c.newRequest(ctx, method, meta, signingRegion)
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

		// S3 can require a bucket-specific region. Re-sign only this request
		// with the advertised region; do not mutate the shared client.
		if (resp.StatusCode == http.StatusMovedPermanently || resp.StatusCode == http.StatusTemporaryRedirect || resp.StatusCode == http.StatusBadRequest) && redirects < 3 {
			if redirectedRegion := resp.Header.Get("X-Amz-Bucket-Region"); redirectedRegion != "" && redirectedRegion != signingRegion {
				_ = resp.Body.Close()
				signingRegion = redirectedRegion
				redirects++
				lastErr = fmt.Errorf("redirected to S3 region %s", redirectedRegion)
				continue
			}
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
