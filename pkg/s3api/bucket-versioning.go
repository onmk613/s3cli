// bucket-versioning.go 实现桶版本控制状态管理: Set/GetBucketVersioning.
// BucketVersioningStatus 类型定义在中立包 s3iface.

package s3api

import (
	"context"
	"encoding/xml"
)

// versioningConfiguration 对应 PutBucketVersioning 请求体.
type versioningConfiguration struct {
	XMLName xml.Name               `xml:"VersioningConfiguration"`
	Status  BucketVersioningStatus `xml:"Status"`
	// MfaDelete 仅 AWS S3 支持, 通常不启用.
	MfaDelete string `xml:"MfaDelete,omitempty"`
}

// getBucketVersioningResult 对应 GetBucketVersioning 响应体.
type getBucketVersioningResult struct {
	XMLName   xml.Name               `xml:"VersioningConfiguration"`
	Status    BucketVersioningStatus `xml:"Status"`
	MfaDelete string                 `xml:"MfaDelete"`
}

// SetBucketVersioning 设置 bucket 的版本控制状态.
//
// status 取值: VersioningEnabled / VersioningSuspended.
func (c *Client) SetBucketVersioning(ctx context.Context, bucket string, status BucketVersioningStatus) error {
	cfg := versioningConfiguration{Status: status}
	body, err := marshalXMLWithHeader(&cfg)
	if err != nil {
		return err
	}
	return c.putBucketSubresource(ctx, bucket, "versioning", body)
}

// GetBucketVersioning 获取 bucket 的版本控制状态.
//
// 若 bucket 从未启用版本控制, 返回空字符串.
func (c *Client) GetBucketVersioning(ctx context.Context, bucket string) (BucketVersioningStatus, error) {
	var result getBucketVersioningResult
	if err := c.getBucketSubresourceXML(ctx, bucket, "versioning", &result); err != nil {
		return "", err
	}
	return result.Status, nil
}
