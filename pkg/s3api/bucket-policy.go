package s3api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// SetBucketPolicy 设置桶的访问策略 (JSON 文档).
func (c *Client) SetBucketPolicy(ctx context.Context, bucket string, data []byte) error {
	if err := checkValidBucketNameStrict(bucket); err != nil {
		return err
	}

	if err := json.Unmarshal(data, new(interface{})); err != nil {
		return err
	}

	urlValues := make(url.Values)
	urlValues.Set("policy", "")

	reqMeta := requestMetadata{
		bucketName:       bucket,
		queryValues:      urlValues,
		contentBody:      strings.NewReader(string(data)),
		contentLength:    int64(len(data)),
		contentMD5Base64: sumMD5Base64(data),
		contentSHA256Hex: sumSHA256Hex(data),
		customHeader: http.Header{
			"Content-Type": []string{"application/json"},
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

// GetBucketPolicy 获取桶的访问策略 (返回原始 JSON 文档).
func (c *Client) GetBucketPolicy(ctx context.Context, bucket string) ([]byte, error) {
	if err := checkValidBucketNameStrict(bucket); err != nil {
		return nil, err
	}

	urlValues := make(url.Values)
	urlValues.Set("policy", "")

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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return body, nil
}

// DeleteBucketPolicy 删除桶的访问策略.
func (c *Client) DeleteBucketPolicy(ctx context.Context, bucket string) error {
	if err := checkValidBucketNameStrict(bucket); err != nil {
		return err
	}

	urlValues := make(url.Values)
	urlValues.Set("policy", "")

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
