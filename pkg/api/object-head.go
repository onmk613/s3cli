// object-head.go 实现 HeadObject (获取对象元数据, 不下载 body) 与 HeadBucket (探活桶),
// 对象元数据全部来自 HTTP 响应头.

package api

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HeadObjectOutput 类型别名定义在 s3iface_types.go.

// HeadObject 获取对象元数据 (不下载 body); 元数据全部来自 HTTP 响应头.
//
// 可选 versionID 指定对象版本. 当前不支持条件请求头 (If-Match / If-None-Match /
// If-Modified-Since / If-Unmodified-Since); 如需条件获取请使用 GetObject.
func (c *Client) HeadObject(ctx context.Context, bucket, key string, versionID string) (*HeadObjectOutput, error) {
	urlValues := make(url.Values)
	if versionID != "" {
		urlValues.Set("versionId", versionID)
	}

	reqMeta := requestMetadata{
		bucketName:  bucket,
		objectName:  key,
		queryValues: urlValues,
	}

	resp, err := c.Do(ctx, http.MethodHead, reqMeta)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	return parseHeadObjectHeaders(resp.Header), nil
}

// parseHeadObjectHeaders 从 HTTP 头解析 HeadObject 输出.
func parseHeadObjectHeaders(h http.Header) *HeadObjectOutput {
	out := &HeadObjectOutput{
		ContentType:          h.Get("Content-Type"),
		ContentEncoding:      h.Get("Content-Encoding"),
		ContentDisposition:   h.Get("Content-Disposition"),
		ContentLanguage:      h.Get("Content-Language"),
		CacheControl:         h.Get("Cache-Control"),
		Expires:              h.Get("Expires"),
		ETag:                 trimQuotes(h.Get("ETag")),
		StorageClass:         h.Get("x-amz-storage-class"),
		VersionID:            h.Get("x-amz-version-id"),
		DeleteMarker:         h.Get("x-amz-delete-marker") == "true",
		ServerSideEncryption: h.Get("x-amz-server-side-encryption"),
		SSEKMSKeyID:          h.Get("x-amz-server-side-encryption-aws-kms-key-id"),
		SSECustomerAlgorithm: h.Get("x-amz-server-side-encryption-customer-algorithm"),
		SSECustomerKeyMD5:    h.Get("x-amz-server-side-encryption-customer-key-MD5"),
		ReplicationStatus:    h.Get("x-amz-replication-status"),
		ObjectLockMode:       h.Get("x-amz-object-lock-mode"),
		ObjectLockLegalHold:  h.Get("x-amz-object-lock-legal-hold"),
	}

	if cl := h.Get("Content-Length"); cl != "" {
		out.ContentLength = parseInt64(cl)
	}

	if h.Get("x-amz-mp-parts-count") != "" {
		out.PartsCount = int32(parseInt64(h.Get("x-amz-mp-parts-count")))
	}

	if lm := h.Get("Last-Modified"); lm != "" {
		if t, err := time.Parse(time.RFC1123, lm); err == nil {
			out.LastModified = t
		}
	}

	if r := h.Get("x-amz-object-lock-retain-until-date"); r != "" {
		if t, err := time.Parse(time.RFC3339, r); err == nil {
			out.ObjectLockRetainUntilDate = t
		}
	}

	// 用户元数据: x-amz-meta-*
	out.Metadata = make(map[string]string)
	for k, vv := range h {
		const prefix = "x-amz-meta-"
		if len(k) > len(prefix) && http.CanonicalHeaderKey(k[:len(prefix)]) == "X-Amz-Meta-" {
			// 规范化头键为驼峰 (X-Amz-Meta-Foo), 上传时原样拼接小写;
			// 统一转小写, 保证 put foo=... -> stat/get 读回 foo 而不是 Foo。
			metaKey := strings.ToLower(k[len(prefix):])
			if len(vv) > 0 {
				out.Metadata[metaKey] = vv[0]
			}
		}
	}
	return out
}

// HeadBucket 检查 bucket 是否存在且可访问.
//
// 成功返回 nil; 不存在返回 *ErrorResponse (Code=NoSuchBucket / AccessDenied).
func (c *Client) HeadBucket(ctx context.Context, bucket string) error {
	reqMeta := requestMetadata{
		bucketName: bucket,
	}

	resp, err := c.Do(ctx, http.MethodHead, reqMeta)
	if err != nil {
		return err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)
	return nil
}
