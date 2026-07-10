package s3api

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"time"
)

// GetObjectOutput 是 GetObject 的返回结构.
type GetObjectOutput struct {
	Body                      io.ReadCloser
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
	AcceptRanges              string
	Metadata                  map[string]string // x-amz-meta-*
}

// GetObjectOptions 控制 GetObject 的可选参数.
type GetObjectOptions struct {
	VersionID                  string
	Range                      string // HTTP Range header, e.g. "bytes=0-1023"
	IfMatch                    string
	IfNoneMatch                string
	IfModifiedSince            *time.Time
	IfUnmodifiedSince          *time.Time
	ResponseContentType        string
	ResponseContentEncoding    string
	ResponseContentDisposition string
	ResponseCacheControl       string
	ResponseExpires            string
}

// GetObject 下载对象, 返回 body 流 (调用方负责关闭).
func (c *Client) GetObject(ctx context.Context, bucket, key string, opts *GetObjectOptions) (*GetObjectOutput, error) {
	if opts == nil {
		opts = &GetObjectOptions{}
	}

	urlValues := make(url.Values)
	if opts.VersionID != "" {
		urlValues.Set("versionId", opts.VersionID)
	}
	// Response-* 覆盖参数
	if opts.ResponseContentType != "" {
		urlValues.Set("response-content-type", opts.ResponseContentType)
	}
	if opts.ResponseContentEncoding != "" {
		urlValues.Set("response-content-encoding", opts.ResponseContentEncoding)
	}
	if opts.ResponseContentDisposition != "" {
		urlValues.Set("response-content-disposition", opts.ResponseContentDisposition)
	}
	if opts.ResponseCacheControl != "" {
		urlValues.Set("response-cache-control", opts.ResponseCacheControl)
	}
	if opts.ResponseExpires != "" {
		urlValues.Set("response-expires", opts.ResponseExpires)
	}

	header := make(http.Header)
	if opts.Range != "" {
		header.Set("Range", opts.Range)
	}
	if opts.IfMatch != "" {
		header.Set("If-Match", opts.IfMatch)
	}
	if opts.IfNoneMatch != "" {
		header.Set("If-None-Match", opts.IfNoneMatch)
	}
	if opts.IfModifiedSince != nil {
		header.Set("If-Modified-Since", opts.IfModifiedSince.UTC().Format(time.RFC1123))
	}
	if opts.IfUnmodifiedSince != nil {
		header.Set("If-Unmodified-Since", opts.IfUnmodifiedSince.UTC().Format(time.RFC1123))
	}

	reqMeta := requestMetadata{
		bucketName:   bucket,
		objectName:   key,
		queryValues:  urlValues,
		customHeader: header,
	}

	resp, err := c.Do(ctx, http.MethodGet, reqMeta)
	if err != nil {
		return nil, err
	}

	out := &GetObjectOutput{
		Body:                 resp.Body,
		ContentType:          resp.Header.Get("Content-Type"),
		ContentEncoding:      resp.Header.Get("Content-Encoding"),
		ContentDisposition:   resp.Header.Get("Content-Disposition"),
		ContentLanguage:      resp.Header.Get("Content-Language"),
		CacheControl:         resp.Header.Get("Cache-Control"),
		Expires:              resp.Header.Get("Expires"),
		ETag:                 trimQuotes(resp.Header.Get("ETag")),
		StorageClass:         resp.Header.Get("x-amz-storage-class"),
		VersionID:            resp.Header.Get("x-amz-version-id"),
		DeleteMarker:         resp.Header.Get("x-amz-delete-marker") == "true",
		ServerSideEncryption: resp.Header.Get("x-amz-server-side-encryption"),
		SSEKMSKeyID:          resp.Header.Get("x-amz-server-side-encryption-aws-kms-key-id"),
		SSECustomerAlgorithm: resp.Header.Get("x-amz-server-side-encryption-customer-algorithm"),
		SSECustomerKeyMD5:    resp.Header.Get("x-amz-server-side-encryption-customer-key-MD5"),
		ReplicationStatus:    resp.Header.Get("x-amz-replication-status"),
		ObjectLockMode:       resp.Header.Get("x-amz-object-lock-mode"),
		ObjectLockLegalHold:  resp.Header.Get("x-amz-object-lock-legal-hold"),
		AcceptRanges:         resp.Header.Get("Accept-Ranges"),
	}

	if cl := resp.Header.Get("Content-Length"); cl != "" {
		out.ContentLength = parseInt64(cl)
	}
	if cr := resp.Header.Get("Content-Range"); cr != "" && out.ContentLength == 0 {
		// Range 请求时 Content-Length 是分片大小; 若缺失可从 Content-Range 推断
		out.ContentLength = parseContentRangeLength(cr)
	}

	if lm := resp.Header.Get("Last-Modified"); lm != "" {
		if t, err := time.Parse(time.RFC1123, lm); err == nil {
			out.LastModified = t
		}
	}

	if h := resp.Header.Get("x-amz-mp-parts-count"); h != "" {
		out.PartsCount = int32(parseInt64(h))
	}

	if r := resp.Header.Get("x-amz-object-lock-retain-until-date"); r != "" {
		if t, err := time.Parse(time.RFC3339, r); err == nil {
			out.ObjectLockRetainUntilDate = t
		}
	}

	// 用户元数据
	out.Metadata = make(map[string]string)
	for k, vv := range resp.Header {
		const prefix = "x-amz-meta-"
		if len(k) > len(prefix) && http.CanonicalHeaderKey(k[:len(prefix)]) == "X-Amz-Meta-" {
			metaKey := k[len(prefix):]
			if len(vv) > 0 {
				out.Metadata[metaKey] = vv[0]
			}
		}
	}

	return out, nil
}

// parseContentRangeLength 从 "bytes 0-1023/2048" 中解析分片长度.
func parseContentRangeLength(cr string) int64 {
	// 格式: bytes start-end/total
	// 长度 = end - start + 1
	rest := cr
	if idx := indexOfByte(rest, ' '); idx >= 0 {
		rest = rest[idx+1:]
	}
	// rest = "0-1023/2048"
	slash := indexOfByte(rest, '/')
	var rangePart string
	if slash >= 0 {
		rangePart = rest[:slash]
	} else {
		rangePart = rest
	}
	dash := indexOfByte(rangePart, '-')
	if dash < 0 {
		return 0
	}
	s := parseInt64(rangePart[:dash])
	e := parseInt64(rangePart[dash+1:])
	if e >= s && s >= 0 {
		return e - s + 1
	}
	return 0
}

// indexOfByte 返回字节在字符串中的位置, 不存在返回 -1.
func indexOfByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
