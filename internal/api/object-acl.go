// object-acl.go 实现对象级 ACL: GET 返回原始 XML, PUT 走 canned ACL / grant 头
// (与桶级 ACL 共用 buildACLHeader).

package api

import (
	"context"
	"io"
	"net/http"
	"net/url"
)

// GetObjectACL 获取对象的 ACL, 返回原始 XML (调用方自行解析).
func (c *Client) GetObjectACL(ctx context.Context, bucket, key, versionID string) ([]byte, error) {
	q := make(url.Values)
	q.Set("acl", "")
	if versionID != "" {
		q.Set("versionId", versionID)
	}
	reqMeta := requestMetadata{
		bucketName:  bucket,
		objectName:  key,
		queryValues: q,
	}
	resp, err := c.Do(ctx, http.MethodGet, reqMeta)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)
	return io.ReadAll(resp.Body)
}

// PutObjectACL 设置对象的 ACL (canned ACL 或 grant 头).
func (c *Client) PutObjectACL(ctx context.Context, bucket, key, versionID string, opts *ACLOptions) error {
	if opts == nil {
		opts = &ACLOptions{}
	}
	q := make(url.Values)
	q.Set("acl", "")
	if versionID != "" {
		q.Set("versionId", versionID)
	}
	reqMeta := requestMetadata{
		bucketName:   bucket,
		objectName:   key,
		queryValues:  q,
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
