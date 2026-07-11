package s3api

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"s3cli/pkg/s3api/s3utils"
)

// ACLPermission 权限类型.
type ACLPermission string

const (
	// PermissionFullControl 完全控制.
	PermissionFullControl ACLPermission = "FULL_CONTROL"
	// PermissionWrite 写入.
	PermissionWrite ACLPermission = "WRITE"
	// PermissionWriteACP 写 ACL.
	PermissionWriteACP ACLPermission = "WRITE_ACP"
	// PermissionRead 读取.
	PermissionRead ACLPermission = "READ"
	// PermissionReadACP 读 ACL.
	PermissionReadACP ACLPermission = "READ_ACP"
)

// ACLGranteeType 被授权者类型.
type ACLGranteeType string

const (
	// GranteeCanonicalUser 标准用户.
	GranteeCanonicalUser ACLGranteeType = "CanonicalUser"
	// GranteeAmazonCustomerByEmail 邮箱.
	GranteeAmazonCustomerByEmail ACLGranteeType = "AmazonCustomerByEmail"
	// GranteeGroup 组.
	GranteeGroup ACLGranteeType = "Group"
)

// ACLGrantee 被授权者.
type ACLGrantee struct {
	XMLName      xml.Name       `xml:"Grantee"`
	Type         ACLGranteeType `xml:"type,attr"` // xsi:type
	ID           string         `xml:"ID,omitempty"`
	URI          string         `xml:"URI,omitempty"`
	DisplayName  string         `xml:"DisplayName,omitempty"`
	EmailAddress string         `xml:"EmailAddress,omitempty"`
}

// ACLGrant 单条授权.
type ACLGrant struct {
	XMLName    xml.Name      `xml:"Grant"`
	Grantee    ACLGrantee    `xml:"Grantee"`
	Permission ACLPermission `xml:"Permission"`
}

// ACLOwner 所有者.
type ACLOwner struct {
	XMLName     xml.Name `xml:"Owner"`
	ID          string   `xml:"ID"`
	DisplayName string   `xml:"DisplayName,omitempty"`
}

// AccessControlPolicy ACL 策略.
type AccessControlPolicy struct {
	XMLName xml.Name   `xml:"AccessControlPolicy"`
	Owner   ACLOwner   `xml:"Owner"`
	Grants  []ACLGrant `xml:"AccessControlList>Grant"`
}

// SetBucketACL 设置 bucket 的 ACL.
//
// 常用 canned ACL (如 "public-read", "private") 可通过 SetBucketACLWithCanned 使用.
func (c *Client) SetBucketACL(ctx context.Context, bucket string, policy *AccessControlPolicy) error {
	if err := s3utils.CheckValidBucketNameStrict(bucket); err != nil {
		return err
	}

	body, err := xml.Marshal(policy)
	if err != nil {
		return err
	}
	body = append([]byte(xml.Header), body...)

	urlValues := make(url.Values)
	urlValues.Set("acl", "")

	reqMeta := requestMetadata{
		bucketName:       bucket,
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

// SetBucketACLWithCanned 使用预定义 ACL 设置 bucket 的 ACL.
//
// 常用值: private, public-read, public-read-write, authenticated-read.
func (c *Client) SetBucketACLWithCanned(ctx context.Context, bucket, cannedACL string) error {
	if err := s3utils.CheckValidBucketNameStrict(bucket); err != nil {
		return err
	}

	urlValues := make(url.Values)
	urlValues.Set("acl", "")

	reqMeta := requestMetadata{
		bucketName:  bucket,
		queryValues: urlValues,
		customHeader: http.Header{
			"x-amz-acl": []string{cannedACL},
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

// GetBucketACL 获取 bucket 的 ACL.
func (c *Client) GetBucketACL(ctx context.Context, bucket string) (*AccessControlPolicy, error) {
	if err := s3utils.CheckValidBucketNameStrict(bucket); err != nil {
		return nil, err
	}

	urlValues := make(url.Values)
	urlValues.Set("acl", "")

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

	var result AccessControlPolicy
	if err := xmlDecoder(resp.Body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetObjectACL 获取对象的 ACL.
//
// 可选 versionID 指定版本.
func (c *Client) GetObjectACL(ctx context.Context, bucket, key, versionID string) (*AccessControlPolicy, error) {
	urlValues := make(url.Values)
	urlValues.Set("acl", "")
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

	var result AccessControlPolicy
	if err := xmlDecoder(resp.Body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SetObjectACL 设置对象的 ACL.
func (c *Client) SetObjectACL(ctx context.Context, bucket, key string, policy *AccessControlPolicy) error {
	body, err := xml.Marshal(policy)
	if err != nil {
		return err
	}
	body = append([]byte(xml.Header), body...)

	urlValues := make(url.Values)
	urlValues.Set("acl", "")

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

// SetObjectACLWithCanned 使用预定义 ACL 设置对象的 ACL.
func (c *Client) SetObjectACLWithCanned(ctx context.Context, bucket, key, cannedACL string) error {
	urlValues := make(url.Values)
	urlValues.Set("acl", "")

	reqMeta := requestMetadata{
		bucketName:  bucket,
		objectName:  key,
		queryValues: urlValues,
		customHeader: http.Header{
			"x-amz-acl": []string{cannedACL},
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
