package s3api

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
)

// DeleteObjectOutput 是 DeleteObject 的返回结构.
type DeleteObjectOutput struct {
	VersionID    string
	DeleteMarker bool
}

// DeleteObject 删除单个对象.
//
// 可选 versionID 指定要删除的版本; 在启用版本控制的 bucket 上,
// 删除会创建一个 delete marker (DeleteMarker=true).
func (c *Client) DeleteObject(ctx context.Context, bucket, key, versionID string) (*DeleteObjectOutput, error) {
	urlValues := make(url.Values)
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
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	return &DeleteObjectOutput{
		VersionID:    resp.Header.Get("x-amz-version-id"),
		DeleteMarker: resp.Header.Get("x-amz-delete-marker") == "true",
	}, nil
}

// ObjectIdentifier 标识要删除的对象 (key + 可选 versionID).
type ObjectIdentifier struct {
	Key       string `xml:"Key"`
	VersionID string `xml:"VersionId,omitempty"`
}

// deleteRequest 对应 DeleteObjects 请求体.
type deleteRequest struct {
	XMLName xml.Name           `xml:"Delete"`
	Quiet   bool               `xml:"Quiet,omitempty"`
	Objects []ObjectIdentifier `xml:"Object"`
}

// deletedObject 对应 DeleteObjects 响应中的单个结果.
type deletedObject struct {
	Key          string `xml:"Key"`
	VersionID    string `xml:"VersionId"`
	DeleteMarker bool   `xml:"DeleteMarker"`
}

// deleteError 对应 DeleteObjects 响应中的错误.
type deleteError struct {
	Key     string `xml:"Key"`
	Code    string `xml:"Code"`
	Message string `xml:"Message"`
}

// deleteResult 对应 DeleteObjects 响应体.
type deleteResult struct {
	XMLName xml.Name        `xml:"DeleteResult"`
	Deleted []deletedObject `xml:"Deleted"`
	Errors  []deleteError   `xml:"Error"`
}

// DeleteObjectsOutput 是 DeleteObjects 的返回结构.
type DeleteObjectsOutput struct {
	Deleted []DeletedObject
	Errors  []DeleteObjectError
}

// DeletedObject 描述已成功删除的对象.
type DeletedObject struct {
	Key          string
	VersionID    string
	DeleteMarker bool
}

// DeleteObjectError 描述批量删除中单个对象的失败.
type DeleteObjectError struct {
	Key     string
	Code    string
	Message string
}

// DeleteObjects 批量删除对象 (最多 1000 个).
//
// 返回每个对象的删除结果和错误. quiet=true 时响应不包含已删除对象列表.
func (c *Client) DeleteObjects(ctx context.Context, bucket string, objects []ObjectIdentifier, quiet bool) (*DeleteObjectsOutput, error) {
	dr := deleteRequest{
		Quiet:   quiet,
		Objects: objects,
	}
	body, err := xml.Marshal(&dr)
	if err != nil {
		return nil, err
	}
	body = append([]byte(xml.Header), body...)

	urlValues := make(url.Values)
	urlValues.Set("delete", "")

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

	resp, err := c.Do(ctx, http.MethodPost, reqMeta)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	var result deleteResult
	if err := xmlDecoder(resp.Body, &result); err != nil {
		return nil, err
	}

	out := &DeleteObjectsOutput{}
	for _, d := range result.Deleted {
		out.Deleted = append(out.Deleted, DeletedObject{
			Key:          d.Key,
			VersionID:    d.VersionID,
			DeleteMarker: d.DeleteMarker,
		})
	}
	for _, e := range result.Errors {
		out.Errors = append(out.Errors, DeleteObjectError{
			Key:     e.Key,
			Code:    e.Code,
			Message: e.Message,
		})
	}
	return out, nil
}
