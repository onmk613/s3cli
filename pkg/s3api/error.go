package s3api

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
)

// ErrorResponse 对应 S3 XML 错误响应体.
//
//	<Error>
//	  <Code>NoSuchKey</Code>
//	  <Message>The resource you requested does not exist</Message>
//	  <Resource>/mybucket/myfoto.jpg</Resource>
//	  <RequestId>4442587FB7D0A2F9</RequestId>
//	</Error>
//
// https://docs.aws.amazon.com/AmazonS3/latest/developerguide/ErrorResponses.html
type ErrorResponse struct {
	XMLName    xml.Name `xml:"Error" json:"-"`
	Code       string   `xml:"Code" json:"code,omitempty"`
	Message    string   `xml:"Message" json:"message,omitempty"`
	BucketName string   `xml:"BucketName" json:"bucketName,omitempty"`
	Key        string   `xml:"Key" json:"key,omitempty"`
	Resource   string   `xml:"Resource" json:"resource,omitempty"`
	RequestID  string   `xml:"RequestId" json:"requestId,omitempty"`
	HostID     string   `xml:"HostId" json:"hostId,omitempty"`
	Region     string   `xml:"Region" json:"region,omitempty"`

	// StatusCode 是 HTTP 状态码 (非 XML 字段).
	StatusCode int `xml:"-" json:"statusCode,omitempty"`
}

// Error 实现 error 接口，返回 JSON 字符串.
func (e *ErrorResponse) Error() string {
	b, err := json.Marshal(e)
	if err != nil {
		// 兜底，避免 marshal 失败时丢失信息
		return fmt.Sprintf("%s: %s (status: %d, request-id: %s)",
			e.Code, e.Message, e.StatusCode, e.RequestID)
	}
	return string(b)
}

// parseErrorResponse 从 HTTP 错误响应中解析 S3 错误. 不关闭 resp.Body.
func parseErrorResponse(resp *http.Response, bucketName, objectName string) *ErrorResponse {
	apiErr := &ErrorResponse{StatusCode: resp.StatusCode}

	// 读取有限长度的响应体, 防止异常服务端返回超大 body
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err == nil && len(body) > 0 {
		_ = xml.Unmarshal(body, apiErr)
	}

	// XML 解析失败或无 body 时, 按状态码兜底
	if apiErr.Code == "" {
		switch resp.StatusCode {
		case http.StatusNotFound:
			if objectName != "" {
				apiErr.Code = "NoSuchKey"
				apiErr.Message = "The specified key does not exist."
			} else if bucketName != "" {
				apiErr.Code = "NoSuchBucket"
				apiErr.Message = "The specified bucket does not exist."
			} else {
				apiErr.Code = "NotFound"
				apiErr.Message = resp.Status
			}
		case http.StatusForbidden:
			apiErr.Code = "AccessDenied"
			apiErr.Message = "Access Denied."
		case http.StatusConflict:
			apiErr.Code = "Conflict"
			apiErr.Message = resp.Status
		case http.StatusPreconditionFailed:
			apiErr.Code = "PreconditionFailed"
			apiErr.Message = resp.Status
		default:
			apiErr.Code = resp.Status
			apiErr.Message = resp.Status
		}
	}

	if apiErr.BucketName == "" {
		apiErr.BucketName = bucketName
	}
	if apiErr.Key == "" {
		apiErr.Key = objectName
	}
	if apiErr.RequestID == "" {
		apiErr.RequestID = resp.Header.Get("X-Amz-Request-Id")
	}
	if apiErr.HostID == "" {
		apiErr.HostID = resp.Header.Get("X-Amz-Id-2")
	}
	return apiErr
}
