package s3api

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
)

// SetObjectTagging 设置对象的标签集合.
//
// 可选 versionID 指定版本.
func (c *Client) SetObjectTagging(ctx context.Context, bucket, key string, tags []Tagging, versionID string) error {
	cfg := taggingConfig{}
	cfg.TagSet.Tag = tags
	body, err := xml.Marshal(&cfg)
	if err != nil {
		return err
	}
	body = append([]byte(xml.Header), body...)

	urlValues := make(url.Values)
	urlValues.Set("tagging", "")
	if versionID != "" {
		urlValues.Set("versionId", versionID)
	}

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

	resp, err := c.Do(ctx, http.MethodPut, reqMeta)
	if err != nil {
		return err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)
	return nil
}

// GetObjectTagging 获取对象的标签集合.
//
// 可选 versionID 指定版本.
func (c *Client) GetObjectTagging(ctx context.Context, bucket, key, versionID string) ([]Tagging, error) {
	urlValues := make(url.Values)
	urlValues.Set("tagging", "")
	if versionID != "" {
		urlValues.Set("versionId", versionID)
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

	var result taggingConfig
	if err := xmlDecoder(resp.Body, &result); err != nil {
		return nil, err
	}
	return result.TagSet.Tag, nil
}

// DeleteObjectTagging 删除对象的标签集合.
//
// 可选 versionID 指定版本.
func (c *Client) DeleteObjectTagging(ctx context.Context, bucket, key, versionID string) error {
	urlValues := make(url.Values)
	urlValues.Set("tagging", "")
	if versionID != "" {
		urlValues.Set("versionId", versionID)
	}

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
