// bucket-acl.go 实现桶级 ACL: GET 返回原始 XML, PUT 走 canned ACL / grant 头.
// ACLOptions 类型定义在中立包 s3iface.

package api

import (
	"context"
	"io"
	"net/http"
	"net/url"
)

// buildACLHeader 由 ACLOptions 构造 x-amz-acl / x-amz-grant-* 请求头.
// 供桶与对象级 PutACL 共用.
func buildACLHeader(opts *ACLOptions) http.Header {
	header := make(http.Header)
	if opts == nil {
		return header
	}
	if opts.ACL != "" {
		header.Set("x-amz-acl", opts.ACL)
	}
	if opts.GrantFullControl != "" {
		header.Set("x-amz-grant-full-control", opts.GrantFullControl)
	}
	if opts.GrantRead != "" {
		header.Set("x-amz-grant-read", opts.GrantRead)
	}
	if opts.GrantReadACP != "" {
		header.Set("x-amz-grant-read-acp", opts.GrantReadACP)
	}
	if opts.GrantWrite != "" {
		header.Set("x-amz-grant-write", opts.GrantWrite)
	}
	if opts.GrantWriteACP != "" {
		header.Set("x-amz-grant-write-acp", opts.GrantWriteACP)
	}
	return header
}

// GetBucketACL 获取 bucket 的 ACL, 返回原始 XML (调用方自行解析).
func (c *Client) GetBucketACL(ctx context.Context, bucket string) ([]byte, error) {
	resp, err := c.getBucketSubresource(ctx, bucket, "acl")
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)
	return io.ReadAll(resp.Body)
}

// PutBucketACL 设置 bucket 的 ACL (canned ACL 或 grant 头).
func (c *Client) PutBucketACL(ctx context.Context, bucket string, opts *ACLOptions) error {
	if opts == nil {
		opts = &ACLOptions{}
	}
	urlValues := make(url.Values)
	urlValues.Set("acl", "")

	reqMeta := requestMetadata{
		bucketName:   bucket,
		queryValues:  urlValues,
		customHeader: buildACLHeader(opts),
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
