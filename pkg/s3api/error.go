// error.go 实现从 HTTP 错误响应中解析错误码/消息的 parseErrorResponse.
// ErrorResponse 类型定义在中立包 s3iface (实现 error 接口).

package s3api

import (
	"encoding/xml"
	"io"
	"net/http"
)

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
