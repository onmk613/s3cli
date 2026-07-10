package s3api

import (
	"context"
	"encoding/json"
	"io"
	"net/url"
	"s3cli/pkg/s3api/s3utils"
	"strings"
)

func (c *Client) SetBucketPolicy(ctx context.Context, bucket string, data []byte) error {
	if err := s3utils.CheckValidBucketNameStrict(bucket); err != nil {
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
	}

	resp, err := c.Do(ctx, "PUT", reqMeta)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

func (c *Client) GetBucketPolicy(ctx context.Context, bucket string) ([]byte, error) {
	if err := s3utils.CheckValidBucketNameStrict(bucket); err != nil {
		return nil, err
	}

	urlValues := make(url.Values)
	urlValues.Set("policy", "")

	reqMeta := requestMetadata{
		bucketName:  bucket,
		queryValues: urlValues,
	}

	resp, err := c.Do(ctx, "GET", reqMeta)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return body, nil
}

func (c *Client) DeleteBucketPolicy(ctx context.Context, bucket string) error {
	if err := s3utils.CheckValidBucketNameStrict(bucket); err != nil {
		return err
	}

	urlValues := make(url.Values)
	urlValues.Set("policy", "")

	reqMeta := requestMetadata{
		bucketName:  bucket,
		queryValues: urlValues,
	}

	resp, err := c.Do(ctx, "DELETE", reqMeta)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}
