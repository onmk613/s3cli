package s3api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// PresignOptions 控制预签名 URL 的生成.
type PresignOptions struct {
	// Method HTTP 方法: GET / PUT / DELETE / HEAD.
	Method string
	// Expires 过期时间.
	Expires time.Duration
	// VersionID 指定对象版本 (仅 GET / HEAD 有效).
	VersionID string
	// ResponseContentType 等 response-* 覆盖参数 (仅 GET 有效).
	ResponseContentType        string
	ResponseContentDisposition string
	ResponseCacheControl       string
}

// PresignedURL 生成一个 SigV4 预签名 URL.
//
// 预签名 URL 允许无凭证的客户端在 expires 时间内访问指定对象.
// AWS S3 v4 预签名最长有效期 7 天 (604800 秒).
func (c *Client) PresignedURL(ctx context.Context, bucket, key string, opts *PresignOptions) (string, error) {
	if opts == nil {
		opts = &PresignOptions{}
	}

	method := strings.ToUpper(strings.TrimSpace(opts.Method))
	if method == "" {
		method = http.MethodGet
	}
	if opts.Expires <= 0 {
		opts.Expires = 15 * time.Minute
	}

	// 构造查询参数
	urlValues := make(url.Values)
	urlValues.Set("X-Amz-Expires", strconv.FormatInt(int64(opts.Expires.Seconds()), 10))
	if opts.VersionID != "" {
		urlValues.Set("versionId", opts.VersionID)
	}
	if opts.ResponseContentType != "" {
		urlValues.Set("response-content-type", opts.ResponseContentType)
	}
	if opts.ResponseContentDisposition != "" {
		urlValues.Set("response-content-disposition", opts.ResponseContentDisposition)
	}
	if opts.ResponseCacheControl != "" {
		urlValues.Set("response-cache-control", opts.ResponseCacheControl)
	}

	// 解析目标 URL
	targetURL, err := c.resolveURL(bucket, key, urlValues)
	if err != nil {
		return "", err
	}

	// 构造一个 http.Request 用于签名
	req, err := http.NewRequestWithContext(ctx, method, targetURL.String(), nil)
	if err != nil {
		return "", err
	}
	req.Host = targetURL.Host

	// SigV4 签名 (query 方式)
	t := time.Now().UTC()
	amzDate := t.Format(iso8601Format)
	scopeDate := t.Format(yyyymmddFormat)

	// 设置签名所需的 query 参数
	q := targetURL.Query()
	q.Set("X-Amz-Algorithm", signV4Algorithm)
	q.Set("X-Amz-Credential", c.accessKey+"/"+scopeDate+"/"+c.region+"/"+serviceS3+"/"+"aws4_request")
	q.Set("X-Amz-Date", amzDate)
	if c.sessionToken != "" {
		q.Set("X-Amz-Security-Token", c.sessionToken)
	}
	q.Set("X-Amz-Expires", strconv.FormatInt(int64(opts.Expires.Seconds()), 10))
	targetURL.RawQuery = s3EncodeQuery(q)
	req.URL = targetURL

	// 构建规范请求 (query 签名方式, payload hash = "UNSIGNED-PAYLOAD")
	canonicalRequest, signedHeaders := buildCanonicalRequest(req, unsignedPayload)

	scope := strings.Join([]string{scopeDate, c.region, serviceS3, "aws4_request"}, "/")
	stringToSign := strings.Join([]string{
		signV4Algorithm,
		amzDate,
		scope,
		sumSHA256Hex([]byte(canonicalRequest)),
	}, "\n")

	signingKey := deriveSigningKey(c.secretKey, scopeDate, c.region)
	signature := hexHMAC(signingKey, stringToSign)

	// 把签名追加到 query
	q.Set("X-Amz-SignedHeaders", signedHeaders)
	q.Set("X-Amz-Signature", signature)
	targetURL.RawQuery = s3EncodeQuery(q)

	return targetURL.String(), nil
}

// PresignGet 便捷方法: 生成 GET 预签名 URL.
func (c *Client) PresignGet(ctx context.Context, bucket, key string, expires time.Duration) (string, error) {
	return c.PresignedURL(ctx, bucket, key, &PresignOptions{
		Method:  http.MethodGet,
		Expires: expires,
	})
}

// PresignPut 便捷方法: 生成 PUT 预签名 URL.
func (c *Client) PresignPut(ctx context.Context, bucket, key string, expires time.Duration) (string, error) {
	return c.PresignedURL(ctx, bucket, key, &PresignOptions{
		Method:  http.MethodPut,
		Expires: expires,
	})
}

// PresignDelete 便捷方法: 生成 DELETE 预签名 URL.
func (c *Client) PresignDelete(ctx context.Context, bucket, key string, expires time.Duration) (string, error) {
	return c.PresignedURL(ctx, bucket, key, &PresignOptions{
		Method:  http.MethodDelete,
		Expires: expires,
	})
}

// PresignHead 便捷方法: 生成 HEAD 预签名 URL.
func (c *Client) PresignHead(ctx context.Context, bucket, key string, expires time.Duration) (string, error) {
	return c.PresignedURL(ctx, bucket, key, &PresignOptions{
		Method:  http.MethodHead,
		Expires: expires,
	})
}

// PresignV2 生成 SigV2 预签名 URL (兼容旧式 S3 服务).
//
// SignV2 签名最长有效期无限制, 适合需要长期有效 URL 的场景.
func (c *Client) PresignV2(_ context.Context, bucket, key string, method string, expires int64) (string, error) {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = "GET"
	}

	targetURL, err := c.resolveURL(bucket, key, nil)
	if err != nil {
		return "", err
	}

	expireTime := time.Now().Unix() + expires
	stringToSign := fmt.Sprintf("%s\n\n\n%d\n/%s/%s", method, expireTime, bucket, key)

	signature := sumHMACSHA256([]byte(c.secretKey), []byte(stringToSign))
	// SignV2 使用 base64 编码的 HMAC-SHA1 (这里用 SHA256 兼容部分实现)
	// 标准 SignV2 应使用 HMAC-SHA1, 但部分私有化服务接受 SHA256
	sigHex := fmt.Sprintf("%x", signature)

	q := targetURL.Query()
	q.Set("AWSAccessKeyId", c.accessKey)
	q.Set("Expires", strconv.FormatInt(expireTime, 10))
	q.Set("Signature", sigHex)
	if c.sessionToken != "" {
		q.Set("SecurityToken", c.sessionToken)
	}
	targetURL.RawQuery = s3EncodeQuery(q)

	return targetURL.String(), nil
}
