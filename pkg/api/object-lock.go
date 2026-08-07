// object-lock.go 实现对象级 Object Lock: retention (PUT/GET ?retention)
// 与 legal-hold (PUT/GET ?legal-hold). 桶级默认配置见 bucket-object-lock.go.

package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
)

// objectLockQuery 构造对象级 Object Lock 子资源查询参数.
func objectLockQuery(subresource, versionID string) url.Values {
	q := make(url.Values)
	q.Set(subresource, "")
	if versionID != "" {
		q.Set("versionId", versionID)
	}
	return q
}

// GetObjectRetention 获取对象保留设置.
func (c *Client) GetObjectRetention(ctx context.Context, bucket, key, versionID string) (*ObjectLockRetention, error) {
	reqMeta := requestMetadata{
		bucketName:  bucket,
		objectName:  key,
		queryValues: objectLockQuery("retention", versionID),
	}
	resp, err := c.Do(ctx, http.MethodGet, reqMeta)
	if err != nil {
		return nil, err
	}
	defer func(Body interface{ Close() error }) {
		_ = Body.Close()
	}(resp.Body)

	var result ObjectLockRetention
	if err := xmlDecoder(resp.Body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// PutObjectRetention 设置对象保留设置.
func (c *Client) PutObjectRetention(ctx context.Context, bucket, key, versionID string, retention *ObjectLockRetention) error {
	body, err := marshalXMLWithHeader(retention)
	if err != nil {
		return err
	}
	reqMeta := requestMetadata{
		bucketName:       bucket,
		objectName:       key,
		queryValues:      objectLockQuery("retention", versionID),
		contentBody:      bytes.NewReader(body),
		contentLength:    int64(len(body)),
		contentMD5Base64: sumMD5Base64(body),
		contentSHA256Hex: sumSHA256Hex(body),
		customHeader:     http.Header{"Content-Type": []string{"application/xml"}},
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

// GetObjectLegalHold 获取对象法律留存状态.
func (c *Client) GetObjectLegalHold(ctx context.Context, bucket, key, versionID string) (*ObjectLockLegalHold, error) {
	reqMeta := requestMetadata{
		bucketName:  bucket,
		objectName:  key,
		queryValues: objectLockQuery("legal-hold", versionID),
	}
	resp, err := c.Do(ctx, http.MethodGet, reqMeta)
	if err != nil {
		return nil, err
	}
	defer func(Body interface{ Close() error }) {
		_ = Body.Close()
	}(resp.Body)

	var result ObjectLockLegalHold
	if err := xmlDecoder(resp.Body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// PutObjectLegalHold 设置对象法律留存状态 (ON / OFF).
func (c *Client) PutObjectLegalHold(ctx context.Context, bucket, key, versionID string, hold *ObjectLockLegalHold) error {
	body, err := marshalXMLWithHeader(hold)
	if err != nil {
		return err
	}
	reqMeta := requestMetadata{
		bucketName:       bucket,
		objectName:       key,
		queryValues:      objectLockQuery("legal-hold", versionID),
		contentBody:      bytes.NewReader(body),
		contentLength:    int64(len(body)),
		contentMD5Base64: sumMD5Base64(body),
		contentSHA256Hex: sumSHA256Hex(body),
		customHeader:     http.Header{"Content-Type": []string{"application/xml"}},
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
