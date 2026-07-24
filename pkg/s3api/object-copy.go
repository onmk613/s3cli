package s3api

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
)

// copyObjectResult 对应 CopyObject 响应体.
type copyObjectResult struct {
	XMLName      xml.Name `xml:"CopyObjectResult"`
	ETag         string
	LastModified string
}

// copyObjectError 用于探测 200 响应体内嵌的 <Error>:
// S3 的 CopyObject 可能先返回 200 再在 body 里写入 <Error> (复制中途失败),
// 若按 copyObjectResult 解码, 字段全空且 Decode 不报错, 会被误判为成功。
type copyObjectError struct {
	XMLName xml.Name
	Code    string `xml:"Code"`
	Message string `xml:"Message"`
}

// CopyObjectOutput 是 CopyObject 的返回结构.
type CopyObjectOutput struct {
	ETag                 string
	LastModified         string
	VersionID            string
	ServerSideEncryption string
	SSEKMSKeyID          string
}

// CopyObjectOptions 控制 CopyObject 的可选参数.
type CopyObjectOptions struct {
	// 源对象版本
	SourceVersionID string
	// 元数据指令: COPY (默认, 复制源元数据) / REPLACE (使用新元数据)
	MetadataDirective string // COPY / REPLACE
	// 标签指令: COPY (默认) / REPLACE
	TaggingDirective string
	// 新元数据 (仅 MetadataDirective=REPLACE 时生效)
	Metadata map[string]string
	// 新标签 (仅 TaggingDirective=REPLACE 时生效), 格式 "k1=v1&k2=v2"
	Tagging string
	// 新存储类型
	StorageClass string
	// 新 ContentType (仅 MetadataDirective=REPLACE)
	ContentType string
	// 新加密设置
	ServerSideEncryption string
	SSEKMSKeyID          string
	// 条件复制
	IfMatch           string
	IfNoneMatch       string
	IfModifiedSince   string
	IfUnmodifiedSince string
}

// CopyObject 在服务端复制对象 (同 endpoint).
//
// copySource 格式为 "sourceBucket/sourceKey" (需 percent-encode 特殊字符).
func (c *Client) CopyObject(ctx context.Context, srcBucket, srcKey, destBucket, destKey string, opts *CopyObjectOptions) (*CopyObjectOutput, error) {
	if opts == nil {
		opts = &CopyObjectOptions{}
	}

	// x-amz-copy-source: /bucket/key?versionId=xxx
	// versionId 同样需要 percent-encode (MinIO 的 versionId 可能含 + / = 等字符)。
	copySource := "/" + srcBucket + "/" + encodePath(srcKey)
	if opts.SourceVersionID != "" {
		copySource += "?versionId=" + percentEncode(opts.SourceVersionID)
	}

	header := make(http.Header)
	header.Set("x-amz-copy-source", copySource)

	if opts.MetadataDirective != "" {
		header.Set("x-amz-metadata-directive", opts.MetadataDirective)
	}
	if opts.TaggingDirective != "" {
		header.Set("x-amz-tagging-directive", opts.TaggingDirective)
	}
	if opts.Tagging != "" {
		header.Set("x-amz-tagging", opts.Tagging)
	}
	if opts.StorageClass != "" {
		header.Set("x-amz-storage-class", opts.StorageClass)
	}
	if opts.ContentType != "" {
		header.Set("Content-Type", opts.ContentType)
	}
	if opts.ServerSideEncryption != "" {
		header.Set("x-amz-server-side-encryption", opts.ServerSideEncryption)
	}
	if opts.SSEKMSKeyID != "" {
		header.Set("x-amz-server-side-encryption-aws-kms-key-id", opts.SSEKMSKeyID)
	}
	if opts.IfMatch != "" {
		header.Set("x-amz-copy-source-if-match", opts.IfMatch)
	}
	if opts.IfNoneMatch != "" {
		header.Set("x-amz-copy-source-if-none-match", opts.IfNoneMatch)
	}
	if opts.IfModifiedSince != "" {
		header.Set("x-amz-copy-source-if-modified-since", opts.IfModifiedSince)
	}
	if opts.IfUnmodifiedSince != "" {
		header.Set("x-amz-copy-source-if-unmodified-since", opts.IfUnmodifiedSince)
	}
	for k, v := range opts.Metadata {
		header.Set("x-amz-meta-"+k, v)
	}

	reqMeta := requestMetadata{
		bucketName:   destBucket,
		objectName:   destKey,
		customHeader: header,
	}

	resp, err := c.Do(ctx, http.MethodPut, reqMeta)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	// CopyObject 可能在 HTTP 200 的响应体内返回 <Error> (复制中途失败),
	// 响应体很小, 先读出来探测根元素, 避免空 ETag 被误判为复制成功。
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var probe copyObjectError
	if err := xml.Unmarshal(body, &probe); err == nil && probe.XMLName.Local == "Error" {
		return nil, &ErrorResponse{
			StatusCode: resp.StatusCode,
			Code:       probe.Code,
			Message:    probe.Message,
			BucketName: destBucket,
			Key:        destKey,
		}
	}

	var result copyObjectResult
	if err := xml.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if result.ETag == "" {
		return nil, fmt.Errorf("copy %s/%s to %s/%s: unexpected 200 response body %q", srcBucket, srcKey, destBucket, destKey, string(body))
	}

	return &CopyObjectOutput{
		ETag:                 trimQuotes(result.ETag),
		LastModified:         result.LastModified,
		VersionID:            resp.Header.Get("x-amz-version-id"),
		ServerSideEncryption: resp.Header.Get("x-amz-server-side-encryption"),
		SSEKMSKeyID:          resp.Header.Get("x-amz-server-side-encryption-aws-kms-key-id"),
	}, nil
}
