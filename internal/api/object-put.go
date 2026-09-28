// object-put.go 实现对象上传: PutObject (内存 bytes) / PutObjectStream (流式, 不读入全部内容)
// / PutString (便捷字符串上传). 支持元数据、存储类型、服务端加密、对象锁等选项.

package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
)

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

// setChecksumHeaders 按 ChecksumAlgorithm 声明算法并带上内容校验和。
// data 为 nil 时只声明算法 (分片上传由 UploadPart 逐片带校验和)。
func setChecksumHeaders(header http.Header, alg ChecksumAlgorithm, data []byte) {
	if alg == "" {
		return
	}
	header.Set("x-amz-sdk-checksum-algorithm", string(alg))
	if data != nil {
		if v := alg.computeChecksum(data); v != "" {
			header.Set("x-amz-checksum-"+alg.headerName(), v)
		}
	}
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
		Checksum:             checksumValuesFromHeader(resp.Header),
	}
}

// PutObject 上传一个内存中的对象 (body []byte). 适合小对象.
// 大文件请使用 PutObjectStream 以避免整体读入内存.
func (c *Client) PutObject(ctx context.Context, bucket, key string, body []byte, opts *PutObjectOptions) (*PutObjectOutput, error) {
	if opts == nil {
		opts = &PutObjectOptions{}
	}

	header := buildPutHeader(opts)
	setChecksumHeaders(header, opts.ChecksumAlgorithm, body)

	reqMeta := requestMetadata{
		bucketName:       bucket,
		objectName:       key,
		customHeader:     header,
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
//
// body 的所有权仍属于调用方: 本方法不会关闭 body, 重试与 region 重定向会
// 在同一 body 上 Seek 回卷后重发。
func (c *Client) PutObjectStream(ctx context.Context, bucket, key string, body io.ReadSeeker, contentLength int64, opts *PutObjectOptions) (*PutObjectOutput, error) {
	if opts == nil {
		opts = &PutObjectOptions{}
	}
	if body == nil {
		return nil, errors.New("PutObjectStream: body is nil")
	}

	// 防御性校验: contentLength <= 0 时旧实现会把 body 替换成 http.NoBody,
	// 于是一个非空 body 会被静默上传成 0 字节对象 (HTTP 200, 无任何报错)。
	if contentLength <= 0 {
		end, err := body.Seek(0, io.SeekEnd)
		if err != nil {
			return nil, fmt.Errorf("measure request body: %w", err)
		}
		if _, err := body.Seek(0, io.SeekStart); err != nil {
			return nil, fmt.Errorf("rewind request body: %w", err)
		}
		if end != contentLength {
			return nil, fmt.Errorf("PutObjectStream: contentLength %d does not match body length %d", contentLength, end)
		}
	}

	header := buildPutHeader(opts)
	if opts.ChecksumAlgorithm != "" {
		// 流式路径没有内存中的数据可算校验和: 显式做一次顺序读计算后回卷。
		// 此前这里漏掉了校验和头, `put --checksum` 对 < 64MiB 的文件 (全部走
		// PutObjectStream) 静默失效。校验和是显式 opt-in, 多一次顺序读与
		// SHA256 签名预读同级。
		header.Set("x-amz-sdk-checksum-algorithm", string(opts.ChecksumAlgorithm))
		if _, err := body.Seek(0, io.SeekStart); err == nil {
			if v, cerr := ComputeChecksumReader(opts.ChecksumAlgorithm, body); cerr == nil && v != "" {
				header.Set("x-amz-checksum-"+opts.ChecksumAlgorithm.headerName(), v)
			}
			if _, serr := body.Seek(0, io.SeekStart); serr != nil {
				return nil, fmt.Errorf("rewind request body: %w", serr)
			}
		}
	}

	reqMeta := requestMetadata{
		bucketName:    bucket,
		objectName:    key,
		customHeader:  header,
		contentBody:   body,
		contentLength: contentLength,
		// contentSHA256Hex 留空: Do 会对 ReadSeeker 计算一次摘要 (重试不重算) 并回卷.
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
