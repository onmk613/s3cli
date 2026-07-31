//go:build aws

// aws-presign.go 实现 AWS 上的预签名 URL: PresignedURL (SigV4) / PresignV2 (SigV2).
//
// PresignedURL 委托给 SDK 的 PresignClient; PresignV2 因 SDK 不原生支持,
// 提取凭证后手动计算 SigV2 签名.

package awss3

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"s3cli/pkg/s3iface"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func (a *AWS) PresignedURL(ctx context.Context, bucket, key string, opts *s3iface.PresignOptions) (string, error) {
	if opts == nil {
		opts = &s3iface.PresignOptions{}
	}
	method := strings.ToUpper(strings.TrimSpace(opts.Method))
	if method == "" {
		method = http.MethodGet
	}
	expires := opts.Expires
	if expires <= 0 {
		expires = 15 * time.Minute
	}
	presignOpts := func(o *s3.PresignOptions) { o.Expires = expires }

	switch method {
	case http.MethodGet:
		input := &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)}
		if opts.VersionID != "" {
			input.VersionId = aws.String(opts.VersionID)
		}
		if opts.ResponseContentType != "" {
			input.ResponseContentType = aws.String(opts.ResponseContentType)
		}
		if opts.ResponseContentDisposition != "" {
			input.ResponseContentDisposition = aws.String(opts.ResponseContentDisposition)
		}
		if opts.ResponseCacheControl != "" {
			input.ResponseCacheControl = aws.String(opts.ResponseCacheControl)
		}
		req, err := a.presign.PresignGetObject(ctx, input, presignOpts)
		if err != nil {
			return "", sdkErr(err)
		}
		return req.URL, nil
	case http.MethodPut:
		input := &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)}
		req, err := a.presign.PresignPutObject(ctx, input, presignOpts)
		if err != nil {
			return "", sdkErr(err)
		}
		return req.URL, nil
	case http.MethodDelete:
		input := &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)}
		if opts.VersionID != "" {
			input.VersionId = aws.String(opts.VersionID)
		}
		req, err := a.presign.PresignDeleteObject(ctx, input, presignOpts)
		if err != nil {
			return "", sdkErr(err)
		}
		return req.URL, nil
	case http.MethodHead:
		input := &s3.HeadObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)}
		if opts.VersionID != "" {
			input.VersionId = aws.String(opts.VersionID)
		}
		req, err := a.presign.PresignHeadObject(ctx, input, presignOpts)
		if err != nil {
			return "", sdkErr(err)
		}
		return req.URL, nil
	default:
		return "", fmt.Errorf("unsupported presign method %q", method)
	}
}

// PresignV2 生成 SigV2 预签名 URL (兼容旧式 S3 服务).
// SDK 不原生支持 SigV2, 此处提取凭证后手动签名.
func (a *AWS) PresignV2(ctx context.Context, bucket, key, method string, expires int64) (string, error) {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = "GET"
	}
	switch method {
	case http.MethodGet, http.MethodPut, http.MethodDelete, http.MethodHead:
	default:
		return "", fmt.Errorf("unsupported presign v2 method %q", method)
	}
	if expires <= 0 {
		return "", fmt.Errorf("presign v2 expiration must be positive")
	}

	opts := a.client.Options()
	if opts.Credentials == nil {
		return "", fmt.Errorf("no credentials provider")
	}
	creds, err := opts.Credentials.Retrieve(ctx)
	if err != nil {
		return "", err
	}
	if opts.BaseEndpoint == nil {
		return "", fmt.Errorf("endpoint not configured")
	}

	endpointURL, err := url.Parse(*opts.BaseEndpoint)
	if err != nil {
		return "", fmt.Errorf("invalid endpoint: %w", err)
	}

	// 构建目标 URL (path-style)
	target := *endpointURL
	target.Path = "/" + bucket
	if key != "" {
		target.Path += "/" + key
	}

	expireTime := time.Now().Unix() + expires
	canonicalResource := "/" + bucket
	if key != "" {
		canonicalResource += "/" + key
	}

	var amzHeaders string
	if creds.SessionToken != "" {
		amzHeaders = "x-amz-security-token:" + creds.SessionToken + "\n"
	}
	stringToSign := fmt.Sprintf("%s\n\n\n%d\n%s%s", method, expireTime, amzHeaders, canonicalResource)
	mac := hmac.New(sha1.New, []byte(creds.SecretAccessKey))
	_, _ = mac.Write([]byte(stringToSign))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	q := target.Query()
	q.Set("AWSAccessKeyId", creds.AccessKeyID)
	q.Set("Expires", strconv.FormatInt(expireTime, 10))
	q.Set("Signature", signature)
	if creds.SessionToken != "" {
		q.Set("x-amz-security-token", creds.SessionToken)
	}
	target.RawQuery = encodeQueryV2(q)
	return target.String(), nil
}

// encodeQueryV2 按 AWSAccessKeyId / Expires / Signature / x-amz-security-token 的顺序编码.
func encodeQueryV2(v url.Values) string {
	order := []string{"AWSAccessKeyId", "Expires", "Signature", "x-amz-security-token"}
	var parts []string
	for _, k := range order {
		if vals, ok := v[k]; ok {
			for _, val := range vals {
				parts = append(parts, k+"="+url.QueryEscape(val))
			}
		}
	}
	return strings.Join(parts, "&")
}
