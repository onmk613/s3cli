// object-put.go 实现对象上传: PutObject (内存 bytes) / PutObjectStream (流式, 不读入全部内容)
// / PutString (便捷字符串上传). 支持元数据、存储类型、服务端加密、对象锁等选项.

package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
)

// PutObjectOutput / PutObjectOptions 类型别名定义在 s3iface_types.go.

// buildPutHeader 根据 PutObjectOptions 构建请求头. 供 PutObject / PutObjectStream 共用.
func buildPutHeader(opts *PutObjectOptions) http.Header {
	header := make(http.Header)
	if opts.ContentType != "" {
		header.Set("Content-Type", opts.ContentType)
	}
	if opts.ContentEncoding != "" {
		header.Set("Content-Encoding", opts.ContentEncoding)
	}
	if opts.ContentDisposition != "" {
		header.Set("Content-Disposition", opts.ContentDisposition)
	}
	if opts.ContentLanguage != "" {
		header.Set("Content-Language", opts.ContentLanguage)
	}
	if opts.CacheControl != "" {
		header.Set("Cache-Control", opts.CacheControl)
	}
	if opts.StorageClass != "" {
		header.Set("x-amz-storage-class", opts.StorageClass)
	}
	if opts.Tagging != "" {
		header.Set("x-amz-tagging", opts.Tagging)
	}
	if opts.ServerSideEncryption != "" {
		header.Set("x-amz-server-side-encryption", opts.ServerSideEncryption)
	}
	if opts.SSEKMSKeyID != "" {
		header.Set("x-amz-server-side-encryption-aws-kms-key-id", opts.SSEKMSKeyID)
	}
	if opts.ObjectLockMode != "" {
		header.Set("x-amz-object-lock-mode", opts.ObjectLockMode)
	}
	if opts.ObjectLockRetainUntilDate != "" {
		header.Set("x-amz-object-lock-retain-until-date", opts.ObjectLockRetainUntilDate)
	}
	if opts.ObjectLockLegalHold != "" {
		header.Set("x-amz-object-lock-legal-hold", opts.ObjectLockLegalHold)
	}
	for k, v := range opts.Metadata {
		header.Set("x-amz-meta-"+k, v)
	}
	setSSECHeaders(header, "x-amz-", opts.SSECustomerAlgorithm, opts.SSECustomerKey, opts.SSECustomerKeyMD5)
	return header
}

// setSSECHeaders 设置 SSE-C (客户提供密钥) 相关请求头. prefix 为 "x-amz-" 表示目标对象加密头
// (x-amz-server-side-encryption-customer-*); 为 "x-amz-copy-source-" 表示拷贝/分片拷贝的源端解密头.
func setSSECHeaders(header http.Header, prefix, alg, key, md5 string) {
	if alg != "" {
		header.Set(prefix+"server-side-encryption-customer-algorithm", alg)
	}
	if key != "" {
		header.Set(prefix+"server-side-encryption-customer-key", key)
	}
	if md5 != "" {
		header.Set(prefix+"server-side-encryption-customer-key-MD5", md5)
	}
}

// putObjectOutput 从响应头解析 PutObjectOutput.
func putObjectOutput(resp *http.Response) *PutObjectOutput {
	return &PutObjectOutput{
		ETag:                 trimQuotes(resp.Header.Get("ETag")),
		VersionID:            resp.Header.Get("x-amz-version-id"),
		ServerSideEncryption: resp.Header.Get("x-amz-server-side-encryption"),
		SSEKMSKeyID:          resp.Header.Get("x-amz-server-side-encryption-aws-kms-key-id"),
	}
}

// PutObject 上传一个内存中的对象 (body []byte). 适合小对象.
// 大文件请使用 PutObjectStream 以避免整体读入内存.
func (c *Client) PutObject(ctx context.Context, bucket, key string, body []byte, opts *PutObjectOptions) (*PutObjectOutput, error) {
	if opts == nil {
		opts = &PutObjectOptions{}
	}

	reqMeta := requestMetadata{
		bucketName:       bucket,
		objectName:       key,
		customHeader:     buildPutHeader(opts),
		contentBody:      bytes.NewReader(body),
		contentLength:    int64(len(body)),
		contentMD5Base64: sumMD5Base64(body),
		contentSHA256Hex: sumSHA256Hex(body),
	}

	resp, err := c.Do(ctx, http.MethodPut, reqMeta)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	return putObjectOutput(resp), nil
}

// PutObjectStream 以流式方式上传一个对象, body 需实现 io.ReadSeeker (如 *os.File),
// 用于签名摘要计算与失败重试回卷. 不会把整个文件读入内存, 适合大文件.
// contentLength 必须为准确的字节数.
func (c *Client) PutObjectStream(ctx context.Context, bucket, key string, body io.ReadSeeker, contentLength int64, opts *PutObjectOptions) (*PutObjectOutput, error) {
	if opts == nil {
		opts = &PutObjectOptions{}
	}

	reqMeta := requestMetadata{
		bucketName:    bucket,
		objectName:    key,
		customHeader:  buildPutHeader(opts),
		contentBody:   body,
		contentLength: contentLength,
		// contentSHA256Hex 留空: newRequest 会对 ReadSeeker 自动计算摘要并回卷.
	}

	resp, err := c.Do(ctx, http.MethodPut, reqMeta)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	return putObjectOutput(resp), nil
}

// PutString 上传一个字符串对象, 便捷方法.
func (c *Client) PutString(ctx context.Context, bucket, key, body string, opts *PutObjectOptions) (*PutObjectOutput, error) {
	if opts == nil {
		opts = &PutObjectOptions{}
	}
	if opts.ContentType == "" {
		opts.ContentType = "text/plain"
	}
	return c.PutObject(ctx, bucket, key, []byte(body), opts)
}
