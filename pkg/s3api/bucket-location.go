package s3api

import (
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
)

// 兼容大多数厂商的获取 Bucket Location API

// getBucketLocationResult 对应 GetBucketLocation 响应体.
//
// <?xml version="1.0" encoding="UTF-8"?>
// <LocationConstraint>us-west-2</LocationConstraint >
//
// 注意: us-east-1 返回空字符串.
type getBucketLocationResult struct {
	XMLName            xml.Name `xml:"LocationConstraint"`
	LocationConstraint string   `xml:",chardata"`
}

// GetBucketLocation 获取 bucket 所在区域.
//
// 注意: AWS S3 的 us-east-1 区域返回空字符串.
func (c *Client) GetBucketLocation(ctx context.Context, bucket string) (string, error) {
	if err := checkValidBucketNameStrict(bucket); err != nil {
		return "", err
	}

	urlValues := make(url.Values)
	urlValues.Set("location", "")

	reqMeta := requestMetadata{
		bucketName:  bucket,
		queryValues: urlValues,
	}

	resp, err := c.Do(ctx, http.MethodGet, reqMeta)
	if err != nil {
		return "", err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	var result getBucketLocationResult
	if err := xmlDecoder(resp.Body, &result); err != nil {
		return "", err
	}
	return result.LocationConstraint, nil
}
