package s3api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strconv"
	"strings"
)

func (c *Client) SetBucketQuota(ctx context.Context, bucket string, quota int64) error {
	if c.vendor != ProviderMinIO {
		return errors.New("set bucket quota is only supported for MinIO server")
	}

	if err := checkValidBucketNameStrict(bucket); err != nil {
		return err
	}

	urlValues := make(url.Values)
	urlValues.Set("bucket", bucket)

	var data []byte
	if quota <= 0 {
		data = []byte(`{"quota": 0}`)
	} else {
		data = []byte(`{"quota": ` + strconv.FormatInt(quota, 10) + `, "quotatype": "hard"}`)
	}

	reqMeta := requestMetadata{
		bucketName:       bucket,
		queryValues:      urlValues,
		customPath:       "/minio/admin/v3/set-bucket-quota",
		contentBody:      strings.NewReader(string(data)),
		contentLength:    int64(len(data)),
		contentMD5Base64: sumMD5Base64(data),
		contentSHA256Hex: sumSHA256Hex(data),
	}

	resp, err := c.Do(ctx, "PUT", reqMeta)
	if err != nil {
		return err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	return nil
}

func (c *Client) InfoBucketQuota(ctx context.Context, bucket string) (int64, error) {
	if c.vendor != ProviderMinIO {
		return 0, errors.New("info bucket quota is only supported for MinIO server")
	}

	if err := checkValidBucketNameStrict(bucket); err != nil {
		return 0, err
	}

	urlValues := make(url.Values)
	urlValues.Set("bucket", bucket)

	reqMeta := requestMetadata{
		bucketName:  bucket,
		queryValues: urlValues,
		customPath:  "/minio/admin/v3/get-bucket-quota",
	}

	resp, err := c.Do(ctx, "GET", reqMeta)
	if err != nil {
		return 0, err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	var result struct {
		Quota int64 `json:"quota"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return 0, err
	}
	return result.Quota, nil
}
