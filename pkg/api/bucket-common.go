// bucket-common.go 实现桶基础操作 (ListBuckets / CreateBucket / DeleteBucket)
// 以及桶子资源 (subresource) 的 PUT/GET/DELETE 通用辅助函数.
// CORS / 加密 / 标签 / 版本 / 通知 / 生命周期 等具体子资源在此基础上构建.

package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
)

// ListBuckets 列出当前凭证下所有可访问的 bucket.
//
// 对应 GET / (ListBuckets), 返回所有者及其可访问的全部 bucket.
func (c *Client) ListBuckets(ctx context.Context) ([]BucketInfo, error) {
	resp, err := c.Do(ctx, http.MethodGet, requestMetadata{})
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	var result listAllMyBucketsResult
	if err := xmlDecoder(resp.Body, &result); err != nil {
		return nil, err
	}

	return result.Buckets.Bucket, nil
}

// listAllMyBucketsResult 对应 ListBuckets 响应体根节点.
//
//	<ListAllMyBucketsResult>
//	  <Owner><ID>...</ID><DisplayName>...</DisplayName></Owner>
//	  <Buckets><Bucket><Name>...</Name><CreationDate>...</CreationDate></Bucket></Buckets>
//	</ListAllMyBucketsResult>
type listAllMyBucketsResult struct {
	Owner   owner
	Buckets struct {
		Bucket []BucketInfo
	}
}

// CreateBucket 创建一个新的 bucket.
func (c *Client) CreateBucket(ctx context.Context, bucketName string, opts *MakeBucketOptions) error {
	if err := checkValidBucketNameStrict(bucketName); err != nil {
		return err
	}

	if opts == nil {
		opts = &MakeBucketOptions{}
	}

	if opts.Region == "" {
		opts.Region = "us-east-1"
		if c.region != "" {
			opts.Region = c.region
		}
	}

	reqMeta := requestMetadata{
		bucketName:     bucketName,
		bucketLocation: opts.Region,
	}

	if opts.ObjectLocking {
		reqMeta.customHeader = http.Header{}
		reqMeta.customHeader.Set("x-amz-bucket-object-lock-enabled", "true")
	}

	resp, err := c.Do(ctx, http.MethodPut, reqMeta)
	if err != nil {
		return err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)
	return nil
}

// DeleteBucket 删除一个 bucket. 该 bucket 必须为空.
func (c *Client) DeleteBucket(ctx context.Context, bucketName string) error {
	if err := checkValidBucketNameStrict(bucketName); err != nil {
		return err
	}

	reqMeta := requestMetadata{
		bucketName: bucketName,
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

// ============================================================================
// 桶子资源 (subresource) 通用辅助
//
// CORS / 加密 / 标签 / 版本 / 通知 / 生命周期 等桶级配置都遵循同一 REST 模式:
//   PUT    /bucket?<subresource>   写入 XML 配置
//   GET    /bucket?<subresource>   读取 XML 配置
//   DELETE /bucket?<subresource>   删除配置
// 以下三个辅助函数消除各配置文件中重复的样板代码.
// ============================================================================

// putBucketSubresource 以 PUT 方式写入桶子资源配置 (body 为已序列化的 XML).
func (c *Client) putBucketSubresource(ctx context.Context, bucket, subresource string, body []byte) error {
	if err := checkValidBucketNameStrict(bucket); err != nil {
		return err
	}

	urlValues := make(url.Values)
	urlValues.Set(subresource, "")

	reqMeta := requestMetadata{
		bucketName:       bucket,
		queryValues:      urlValues,
		contentBody:      bytes.NewReader(body),
		contentLength:    int64(len(body)),
		contentMD5Base64: sumMD5Base64(body),
		contentSHA256Hex: sumSHA256Hex(body),
		customHeader: http.Header{
			"Content-Type": []string{"application/xml"},
		},
	}

	resp, err := c.Do(ctx, http.MethodPut, reqMeta)
	if err != nil {
		return err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)
	return nil
}

// getBucketSubresource 以 GET 方式读取桶子资源配置, 返回响应供调用方解码.
// 调用方负责关闭返回的 resp.Body.
func (c *Client) getBucketSubresource(ctx context.Context, bucket, subresource string) (*http.Response, error) {
	if err := checkValidBucketNameStrict(bucket); err != nil {
		return nil, err
	}

	urlValues := make(url.Values)
	urlValues.Set(subresource, "")

	reqMeta := requestMetadata{
		bucketName:  bucket,
		queryValues: urlValues,
	}

	return c.Do(ctx, http.MethodGet, reqMeta)
}

// getBucketSubresourceXML 读取桶子资源配置并将 XML 响应体解码到 out.
func (c *Client) getBucketSubresourceXML(ctx context.Context, bucket, subresource string, out any) error {
	resp, err := c.getBucketSubresource(ctx, bucket, subresource)
	if err != nil {
		return err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)
	return xmlDecoder(resp.Body, out)
}

// deleteBucketSubresource 以 DELETE 方式删除桶子资源配置.
func (c *Client) deleteBucketSubresource(ctx context.Context, bucket, subresource string) error {
	if err := checkValidBucketNameStrict(bucket); err != nil {
		return err
	}

	urlValues := make(url.Values)
	urlValues.Set(subresource, "")

	reqMeta := requestMetadata{
		bucketName:  bucket,
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
