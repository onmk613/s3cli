package s3api

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// HeadObjectOutput 是 HeadObject 的返回结构.
//
// HeadObject 无响应体, 所有元数据来自 HTTP 响应头.
type HeadObjectOutput struct {
	ContentLength             int64
	ContentType               string
	ContentEncoding           string
	ContentDisposition        string
	ContentLanguage           string
	CacheControl              string
	Expires                   string
	ETag                      string
	LastModified              time.Time
	StorageClass              string
	VersionID                 string
	DeleteMarker              bool
	ServerSideEncryption      string
	SSEKMSKeyID               string
	SSECustomerAlgorithm      string
	SSECustomerKeyMD5         string
	PartsCount                int32
	ReplicationStatus         string
	ObjectLockMode            string
	ObjectLockRetainUntilDate time.Time
	ObjectLockLegalHold       string
	Metadata                  map[string]string // x-amz-meta-*
}

// HeadObject 获取对象元数据 (不下载 body).
//
// 可选 versionID 指定版本. 可选 condition 参数支持 If-Match / If-None-Match / If-Modified-Since 等.
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
			metaKey := k[len(prefix):]
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

// trimQuotes 去掉 ETag 周围的引号.
func trimQuotes(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

// parseInt64 安全解析 int64.
func parseInt64(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}
