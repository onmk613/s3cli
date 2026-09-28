// bucket-location.go 实现 GetBucketLocation, 查询 bucket 所在区域.
// 兼容 AWS S3 及大多数 S3 兼容厂商; 注意 AWS us-east-1 区域返回空字符串.

package api

import (
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
)

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
	// AWS 对 eu-west-1 的桶返回遗留值 "EU"; 该值会进 bucketLocCache 并被用于
	// %(region) 模板寻址与签名 scope, 必须映射为真实区域名, 否则错 host + 错签名。
	if result.LocationConstraint == "EU" {
		return "eu-west-1", nil
	}
	return result.LocationConstraint, nil
}
