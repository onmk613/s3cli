package s3api

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// CreateMultipartUploadOutput 是 CreateMultipartUpload 的返回结构.
type CreateMultipartUploadOutput struct {
	UploadID             string
	ServerSideEncryption string
	SSEKMSKeyID          string
}

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

// UploadPartOutput 是 UploadPart 的返回结构.
type UploadPartOutput struct {
	ETag                 string
	SSEKMSKeyID          string
	ServerSideEncryption string
}

// UploadPart 上传一个分片.
//
// partNumber 从 1 开始, 最大 10000. body 需可 Seek.
func (c *Client) UploadPart(ctx context.Context, bucket, key, uploadID string, partNumber int, body []byte) (*UploadPartOutput, error) {
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

// CompletedPart 已上传完成的分片信息.
type CompletedPart struct {
	XMLName    xml.Name `xml:"Part"`
	PartNumber int      `xml:"PartNumber"`
	ETag       string   `xml:"ETag"`
}

// completedMultipartUpload 对应 CompleteMultipartUpload 请求体.
type completedMultipartUpload struct {
	XMLName xml.Name        `xml:"CompleteMultipartUpload"`
	Parts   []CompletedPart `xml:"Part"`
}

// CompleteMultipartUploadOutput 是 CompleteMultipartUpload 的返回结构.
type CompleteMultipartUploadOutput struct {
	Location             string
	Bucket               string
	Key                  string
	ETag                 string
	VersionID            string
	ServerSideEncryption string
	SSEKMSKeyID          string
}

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
// parts 必须按 partNumber 升序排列.
func (c *Client) CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string, parts []CompletedPart) (*CompleteMultipartUploadOutput, error) {
	body, err := xml.Marshal(&completedMultipartUpload{Parts: parts})
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

	var result completeMultipartUploadResult
	if err := xmlDecoder(resp.Body, &result); err != nil {
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

// uploadInfo 单个进行中的分片上传.
type uploadInfo struct {
	XMLName      xml.Name `xml:"Upload"`
	Key          string
	UploadID     string `xml:"UploadId"`
	Initiated    time.Time
	StorageClass string
}

// ListMultipartUploadsOptions 控制 ListMultipartUploads 的可选参数.
type ListMultipartUploadsOptions struct {
	Prefix         string
	Delimiter      string
	KeyMarker      string
	UploadIDMarker string
	MaxUploads     int
}

// ListMultipartUploadsOutput 是 ListMultipartUploads 的返回结构.
type ListMultipartUploadsOutput struct {
	Bucket             string
	KeyMarker          string
	UploadIDMarker     string
	NextKeyMarker      string
	NextUploadIDMarker string
	MaxUploads         int
	IsTruncated        bool
	Uploads            []uploadInfo
}

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

// partInfo 单个分片信息.
type partInfo struct {
	XMLName      xml.Name `xml:"Part"`
	PartNumber   int
	LastModified time.Time
	ETag         string
	Size         int64
}

// ListPartsOutput 是 ListParts 的返回结构.
type ListPartsOutput struct {
	Bucket               string
	Key                  string
	UploadID             string
	PartNumberMarker     int
	NextPartNumberMarker int
	MaxParts             int
	IsTruncated          bool
	Parts                []partInfo
}

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
