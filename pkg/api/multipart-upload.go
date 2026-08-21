// multipart-upload.go 实现分片上传完整生命周期:
// CreateMultipartUpload (初始化) -> UploadPart (逐片上传) -> CompleteMultipartUpload (完成)
// 以及 AbortMultipartUpload (中止)、ListMultipartUploads (列出进行中的上传)、
// ListParts (列出已上传分片).

package api

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// maxMultipartPartNumber 是 S3 分片号的协议上限 (1..10000)。
const maxMultipartPartNumber = 10000

// CreateMultipartUploadOutput / UploadPartOutput / CompletedPart / CompleteMultipartUploadOutput /
// ListMultipartUploadsOptions / ListMultipartUploadsOutput / ListPartsOutput 类型别名定义在 s3iface_types.go.

// CreateMultipartUpload 初始化一个分片上传.
//
// opts 中的 ContentType / Metadata 等会作为最终对象的元数据.
func (c *Client) CreateMultipartUpload(ctx context.Context, bucket, key string, opts *PutObjectOptions) (*CreateMultipartUploadOutput, error) {
	if opts == nil {
		opts = &PutObjectOptions{}
	}

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

	urlValues := make(url.Values)
	urlValues.Set("uploads", "")

	reqMeta := requestMetadata{
		bucketName:   bucket,
		objectName:   key,
		queryValues:  urlValues,
		customHeader: header,
	}

	resp, err := c.Do(ctx, http.MethodPost, reqMeta)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	var result struct {
		XMLName  xml.Name `xml:"InitiateMultipartUploadResult"`
		UploadID string   `xml:"UploadId"`
	}
	if err := xmlDecoder(resp.Body, &result); err != nil {
		return nil, err
	}

	return &CreateMultipartUploadOutput{
		UploadID:             result.UploadID,
		ServerSideEncryption: resp.Header.Get("x-amz-server-side-encryption"),
		SSEKMSKeyID:          resp.Header.Get("x-amz-server-side-encryption-aws-kms-key-id"),
	}, nil
}

// UploadPart 上传一个分片.
//
// partNumber 从 1 开始, 最大 10000. body 需可 Seek.
func (c *Client) UploadPart(ctx context.Context, bucket, key, uploadID string, partNumber int, body []byte) (*UploadPartOutput, error) {
	// 客户端先校验分片号, 超界时部分网关返回的 InvalidArgument 缺少上下文,
	// 不如直接给出可定位的报错。
	if partNumber < 1 || partNumber > maxMultipartPartNumber {
		return nil, fmt.Errorf("part number %d out of range [1, %d]", partNumber, maxMultipartPartNumber)
	}

	urlValues := make(url.Values)
	urlValues.Set("partNumber", strconv.Itoa(partNumber))
	urlValues.Set("uploadId", uploadID)

	reqMeta := requestMetadata{
		bucketName:       bucket,
		objectName:       key,
		queryValues:      urlValues,
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

	return &UploadPartOutput{
		ETag:                 trimQuotes(resp.Header.Get("ETag")),
		SSEKMSKeyID:          resp.Header.Get("x-amz-server-side-encryption-aws-kms-key-id"),
		ServerSideEncryption: resp.Header.Get("x-amz-server-side-encryption"),
	}, nil
}

// uploadPartCopyResult 对应 UploadPartCopy 响应体.
type uploadPartCopyResult struct {
	XMLName      xml.Name `xml:"CopyPartResult"`
	ETag         string
	LastModified string
}

// UploadPartCopy 在服务端把源对象的一段拷贝为目标分片上传的一个 part.
// 用于 >5GB 对象的服务端拷贝 (单次 CopyObject 在 AWS 上对 >5GiB 源对象会返回 EntityTooLarge):
// 通过 opts.CopySourceRange ("bytes=start-end") 从源对象截取一段作为一个分片, 服务端零下载完成.
func (c *Client) UploadPartCopy(ctx context.Context, srcBucket, srcKey, destBucket, destKey, uploadID string, partNumber int, opts *UploadPartCopyOptions) (*UploadPartCopyOutput, error) {
	if opts == nil {
		opts = &UploadPartCopyOptions{}
	}

	// x-amz-copy-source: /bucket/key[?versionId=xxx]
	copySource := "/" + srcBucket + "/" + encodePath(srcKey)
	if opts.SrcVersionID != "" {
		copySource += "?versionId=" + percentEncode(opts.SrcVersionID)
	}

	header := make(http.Header)
	header.Set("x-amz-copy-source", copySource)
	if opts.CopySourceRange != "" {
		header.Set("x-amz-copy-source-range", opts.CopySourceRange)
	}
	// 源端 SSE-C 解密 + 目标端 SSE-C 加密
	setSSECHeaders(header, "x-amz-copy-source-", opts.SourceSSECustomerAlgorithm, opts.SourceSSECustomerKey, opts.SourceSSECustomerKeyMD5)
	setSSECHeaders(header, "x-amz-", opts.SSECustomerAlgorithm, opts.SSECustomerKey, opts.SSECustomerKeyMD5)

	urlValues := make(url.Values)
	urlValues.Set("partNumber", strconv.Itoa(partNumber))
	urlValues.Set("uploadId", uploadID)

	reqMeta := requestMetadata{
		bucketName:   destBucket,
		objectName:   destKey,
		queryValues:  urlValues,
		customHeader: header,
	}

	resp, err := c.Do(ctx, http.MethodPut, reqMeta)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	// S3 可能在 HTTP 200 的响应体内嵌入 <Error> (复制中途失败), 先探测避免误判成功。
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

	var result uploadPartCopyResult
	if err := xml.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	out := &UploadPartCopyOutput{
		ETag:                 trimQuotes(result.ETag),
		ServerSideEncryption: resp.Header.Get("x-amz-server-side-encryption"),
		SSEKMSKeyID:          resp.Header.Get("x-amz-server-side-encryption-aws-kms-key-id"),
	}
	if result.LastModified != "" {
		if t, err := time.Parse(time.RFC3339, result.LastModified); err == nil {
			out.LastModified = t
		}
	}
	return out, nil
}

// CompletedPart 类型别名定义在 s3iface_types.go.

// completedMultipartUpload 对应 CompleteMultipartUpload 请求体.
type completedMultipartUpload struct {
	XMLName xml.Name        `xml:"CompleteMultipartUpload"`
	Parts   []CompletedPart `xml:"Part"`
}

// CompleteMultipartUploadOutput 类型别名定义在 s3iface_types.go.

// completeMultipartUploadResult 对应 CompleteMultipartUpload 响应体.
type completeMultipartUploadResult struct {
	XMLName  xml.Name `xml:"CompleteMultipartUploadResult"`
	Location string
	Bucket   string
	Key      string
	ETag     string
}

// CompleteMultipartUpload 完成分片上传.
//
// parts 必须按 partNumber 升序排列; parts 中 ETag 允许带引号,
// 序列化前统一去引号 (UploadPart 的输出已去引号, 此处兜底调用方自组 ETag 的场景).
func (c *Client) CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string, parts []CompletedPart) (*CompleteMultipartUploadOutput, error) {
	// S3 要求 parts 严格升序; 乱序会收到无上下文的 InvalidPartOrder,
	// 客户端先校验并指出位置, 同时兜底分片号范围。
	for i, p := range parts {
		if p.PartNumber < 1 || p.PartNumber > maxMultipartPartNumber {
			return nil, fmt.Errorf("part %d: part number %d out of range [1, %d]", i, p.PartNumber, maxMultipartPartNumber)
		}
		if i > 0 && p.PartNumber <= parts[i-1].PartNumber {
			return nil, fmt.Errorf("parts must be in ascending order: part %d (number %d) follows number %d", i, p.PartNumber, parts[i-1].PartNumber)
		}
	}
	normalized := make([]CompletedPart, len(parts))
	for i, p := range parts {
		p.ETag = trimQuotes(p.ETag)
		normalized[i] = p
	}
	body, err := xml.Marshal(&completedMultipartUpload{Parts: normalized})
	if err != nil {
		return nil, err
	}
	body = append([]byte(xml.Header), body...)

	urlValues := make(url.Values)
	urlValues.Set("uploadId", uploadID)

	reqMeta := requestMetadata{
		bucketName:       bucket,
		objectName:       key,
		queryValues:      urlValues,
		contentBody:      bytes.NewReader(body),
		contentLength:    int64(len(body)),
		contentMD5Base64: sumMD5Base64(body),
		contentSHA256Hex: sumSHA256Hex(body),
		customHeader: http.Header{
			"Content-Type": []string{"application/xml"},
		},
	}

	resp, err := c.Do(ctx, http.MethodPost, reqMeta)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	// AWS 文档明确: CompleteMultipartUpload 可能先返回 200, 再在 body 内嵌入
	// <Error> (如拼装中途 InternalError)。不探测的话错误会被解码成
	// "expected element <CompleteMultipartUploadResult>", 丢失 Code/Message。
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var probe copyObjectError
	if err := xml.Unmarshal(respBody, &probe); err == nil && probe.XMLName.Local == "Error" {
		return nil, &ErrorResponse{
			StatusCode: resp.StatusCode,
			Code:       probe.Code,
			Message:    probe.Message,
			BucketName: bucket,
			Key:        key,
		}
	}

	var result completeMultipartUploadResult
	if err := xml.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return &CompleteMultipartUploadOutput{
		Location:             result.Location,
		Bucket:               result.Bucket,
		Key:                  result.Key,
		ETag:                 trimQuotes(result.ETag),
		VersionID:            resp.Header.Get("x-amz-version-id"),
		ServerSideEncryption: resp.Header.Get("x-amz-server-side-encryption"),
		SSEKMSKeyID:          resp.Header.Get("x-amz-server-side-encryption-aws-kms-key-id"),
	}, nil
}

// AbortMultipartUpload 中止分片上传, 释放已上传分片占用的空间.
func (c *Client) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	urlValues := make(url.Values)
	urlValues.Set("uploadId", uploadID)

	reqMeta := requestMetadata{
		bucketName:  bucket,
		objectName:  key,
		queryValues: urlValues,
	}

	resp, err := c.Do(ctx, http.MethodDelete, reqMeta)
	if err != nil {
		return err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)
	return nil
}

// listMultipartUploadsResult 对应 ListMultipartUploads 响应体.
type listMultipartUploadsResult struct {
	XMLName            xml.Name `xml:"ListMultipartUploadsResult"`
	Bucket             string
	KeyMarker          string
	UploadIDMarker     string `xml:"UploadIdMarker"`
	NextKeyMarker      string
	NextUploadIDMarker string `xml:"NextUploadIdMarker"`
	MaxUploads         int
	IsTruncated        bool
	Uploads            []uploadInfo `xml:"Upload"`
}

// uploadInfo 类型别名定义在 s3iface_types.go.

// ListMultipartUploadsOptions 类型别名定义在 s3iface_types.go.

// ListMultipartUploadsOutput 类型别名定义在 s3iface_types.go.

// ListMultipartUploads 列出 bucket 中进行中的分片上传.
func (c *Client) ListMultipartUploads(ctx context.Context, bucket string, opts *ListMultipartUploadsOptions) (*ListMultipartUploadsOutput, error) {
	if opts == nil {
		opts = &ListMultipartUploadsOptions{}
	}

	urlValues := make(url.Values)
	urlValues.Set("uploads", "")
	if opts.Prefix != "" {
		urlValues.Set("prefix", opts.Prefix)
	}
	if opts.Delimiter != "" {
		urlValues.Set("delimiter", opts.Delimiter)
	}
	if opts.KeyMarker != "" {
		urlValues.Set("key-marker", opts.KeyMarker)
	}
	if opts.UploadIDMarker != "" {
		urlValues.Set("upload-id-marker", opts.UploadIDMarker)
	}
	if opts.MaxUploads > 0 {
		urlValues.Set("max-uploads", strconv.Itoa(opts.MaxUploads))
	}

	reqMeta := requestMetadata{
		bucketName:  bucket,
		queryValues: urlValues,
	}

	resp, err := c.Do(ctx, http.MethodGet, reqMeta)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	var result listMultipartUploadsResult
	if err := xmlDecoder(resp.Body, &result); err != nil {
		return nil, err
	}

	return &ListMultipartUploadsOutput{
		Bucket:             result.Bucket,
		KeyMarker:          result.KeyMarker,
		UploadIDMarker:     result.UploadIDMarker,
		NextKeyMarker:      result.NextKeyMarker,
		NextUploadIDMarker: result.NextUploadIDMarker,
		MaxUploads:         result.MaxUploads,
		IsTruncated:        result.IsTruncated,
		Uploads:            result.Uploads,
	}, nil
}

// listPartsResult 对应 ListParts 响应体.
type listPartsResult struct {
	XMLName              xml.Name `xml:"ListPartsResult"`
	Bucket               string
	Key                  string
	UploadID             string `xml:"UploadId"`
	PartNumberMarker     int
	NextPartNumberMarker int
	MaxParts             int
	IsTruncated          bool
	Parts                []partInfo `xml:"Part"`
}

// partInfo 类型别名定义在 s3iface_types.go.

// ListPartsOutput 类型别名定义在 s3iface_types.go.

// ListParts 列出已上传的分片.
func (c *Client) ListParts(ctx context.Context, bucket, key, uploadID string, partNumberMarker, maxParts int) (*ListPartsOutput, error) {
	urlValues := make(url.Values)
	urlValues.Set("uploadId", uploadID)
	if partNumberMarker > 0 {
		urlValues.Set("part-number-marker", strconv.Itoa(partNumberMarker))
	}
	if maxParts > 0 {
		urlValues.Set("max-parts", strconv.Itoa(maxParts))
	}

	reqMeta := requestMetadata{
		bucketName:  bucket,
		objectName:  key,
		queryValues: urlValues,
	}

	resp, err := c.Do(ctx, http.MethodGet, reqMeta)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	var result listPartsResult
	if err := xmlDecoder(resp.Body, &result); err != nil {
		return nil, err
	}

	return &ListPartsOutput{
		Bucket:               result.Bucket,
		Key:                  result.Key,
		UploadID:             result.UploadID,
		PartNumberMarker:     result.PartNumberMarker,
		NextPartNumberMarker: result.NextPartNumberMarker,
		MaxParts:             result.MaxParts,
		IsTruncated:          result.IsTruncated,
		Parts:                result.Parts,
	}, nil
}
