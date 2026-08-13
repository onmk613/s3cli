// api.go 定义 S3 客户端核心: Client / Options / 构造函数 New, 以及一次完整
// 请求的生命周期 (requestMetadata -> newRequest 签名 -> Do 发送+重试).
// 签名细节见 signer.go, 错误解析见 error.go.

package api

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

// Client 是一个 S3 兼容客户端, 封装了凭证、寻址方式与 HTTP 传输, 可复用于多次请求.
// 通过 New 构造; 所有 S3 操作均为其方法. Client 并发安全 (构造后不再修改内部状态).
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

// Options 是构造 Client 的参数集合. 传给 New 后部分字段会被填充默认值.
type Options struct {
	// 基础配置
	Endpoint     string // S3 服务入口, 如 "https://s3.example.com" (缺省协议时按 http 处理)
	AccessKey    string // 访问密钥 ID
	SecretKey    string // 访问密钥
	SessionToken string // 临时凭证会话令牌 (STS), 永久凭证留空

	// 地区和厂商
	Region string // 服务区域, 缺省为 us-east-1

	// bucket 寻址方式和自定义寻址函数
	BucketLookup       BucketLookupType // path / DNS / auto 寻址
	BucketLookupViaURL BucketLookupFunc // 自定义寻址模板, 优先级高于 BucketLookup

	// 自定义http.Transport, 用于注入自定义header / 代理 / 证书等.
	Transport http.RoundTripper

	// 最大重试次数
	MaxRetries int // 失败重试次数, <=0 时默认 3
}

// New 根据	opts 构造一个 S3 客户端. 必填字段为 Endpoint / AccessKey / SecretKey.
// 未提供的可选字段会使用合理默认值 (region=us-east-1, 重试 3 次, path-style 寻址等).
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
		httpClient: &http.Client{
			Transport: transport,
			// 禁止自动跟随 301/307: S3 的 region 重定向需要重签 (region 变化),
			// 默认跟随会跨 host 转发且丢失 Authorization 头 (未签名重发)。
			// 让 Do 收到 3xx 响应后自行解析 X-Amz-Bucket-Region 并重签重发。
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		maxRetries: opts.MaxRetries,
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
	} else if meta.contentBody != nil {
		// 0 字节 body (如空对象上传): 显式替换为 http.NoBody, 保证发送
		// "Content-Length: 0" 而非 chunked 编码。http.NewRequest 只会对实现了
		// Len() 的空 reader (如 bytes.Reader) 自动做此转换; 其它类型 (如
		// PutObjectStream 传入的 0 长度 *os.File) 的 ContentLength 为 0 时会被
		// transport 视为长度未知而走 chunked, 严格服务端会拒绝。
		req.Body = http.NoBody
		req.ContentLength = 0
		req.GetBody = nil
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
	// 若 body 可 Seek, 记录起点以便重试/重定向回卷
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
	// 优先复用已缓存的 bucket region (来自此前的 region 重定向或 GetBucketLocation 探测),
	// 避免每次请求都先触发一次重定向。service 级请求 (bucket 为空) 用配置 region。
	signingRegion := c.region
	if meta.bucketName != "" {
		if v, ok := c.bucketLocCache.Get(meta.bucketName); ok {
			signingRegion = v
		}
	}
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

		// 内层循环处理 region 重定向: 在同一 attempt 上重签重发, 不消耗重试预算,
		// 把完整的重试次数留给真正的网络/5xx 错误。
		var resp *http.Response
		for {
			req, err := c.newRequest(ctx, method, meta, signingRegion)
			if err != nil {
				return nil, err
			}

			resp, err = c.httpClient.Do(req)
			if err != nil {
				lastErr = err
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				break // 网络层错误, 走外层重试
			}

			// S3 可能要求 bucket 特定 region (301/307 或带 X-Amz-Bucket-Region 的 400)。
			// 只重签本次请求, 不修改共享 client; 并把发现的 region 写入缓存,
			// 后续该 bucket 的请求直接命中正确 region, 无需再次重定向。
			if (resp.StatusCode == http.StatusMovedPermanently || resp.StatusCode == http.StatusTemporaryRedirect || resp.StatusCode == http.StatusBadRequest) && redirects < 3 {
				if redirectedRegion := resp.Header.Get("X-Amz-Bucket-Region"); redirectedRegion != "" && redirectedRegion != signingRegion {
					_ = resp.Body.Close()
					signingRegion = redirectedRegion
					if meta.bucketName != "" {
						c.bucketLocCache.Set(meta.bucketName, redirectedRegion)
					}
					redirects++
					lastErr = fmt.Errorf("redirected to S3 region %s", redirectedRegion)
					// 回卷 body 以便重签后重发; 不可回卷则无法安全重发
					if seeker != nil {
						if _, err := seeker.Seek(bodyStart, io.SeekStart); err != nil {
							return nil, err
						}
					} else if meta.contentBody != nil {
						return nil, lastErr
					}
					continue // 内层循环: 同一 attempt 重签重发
				}
			}
			break // 非重定向, 交给下面的成功/错误处理
		}

		// 网络层错误: resp 为 nil, 继续外层重试
		if resp == nil {
			continue
		}

		// 成功 (仅 2xx; 3xx 此处只剩 region 重定向耗尽等异常, 作为错误处理)
		if resp.StatusCode < 300 {
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
	// 移位前 cap attempt: 200ms << 25 约 1.7h, 已远超 retryCap;
	// 避免极大 MaxRetries 时 int64 纳秒移位溢出为负/零。
	if attempt > 25 {
		attempt = 25
	}
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
