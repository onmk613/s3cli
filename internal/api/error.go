// error.go 实现从 HTTP 错误响应中解析错误码/消息的 parseErrorResponse,
// 以及全代码库统一的 S3 错误分类谓词 (IsNotFound / IsAccessDenied / HasCode).
// ErrorResponse 类型定义见 types.go (实现 error 接口).

package api

import (
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"strings"
)

// ErrorOf 返回 err 链上第一个 *ErrorResponse, 不存在时返回 nil。
// S3 错误的判定统一经由这里, 避免各处自行 errors.As + 字符串匹配导致口径漂移。
func ErrorOf(err error) *ErrorResponse {
	if apiErr, ok := errors.AsType[*ErrorResponse](err); ok {
		return apiErr
	}
	return nil
}

// CodeOf 返回 err 链上的 S3 错误码, 不存在时返回 ""。
func CodeOf(err error) string {
	if apiErr := ErrorOf(err); apiErr != nil {
		return apiErr.Code
	}
	return ""
}

// notFoundError 是本地判定的"不存在": 展示文本可自定义, 但 Unwrap 到
// ErrNotFound, 因此 api.IsNotFound 与退出码映射都能正确识别。
type notFoundError struct{ msg string }

func (e *notFoundError) Error() string { return e.msg }
func (e *notFoundError) Unwrap() error { return ErrNotFound }

// ErrNotFound 是"资源不存在"的哨兵错误, 供 IsNotFound 识别。
var ErrNotFound = &ErrorResponse{Code: "NoSuchKey", StatusCode: http.StatusNotFound}

// NewNotFound 构造一个可被 IsNotFound 识别的本地"不存在"错误。
// 用于已经确认资源不存在、但错误链上没有服务端响应的路径判定逻辑。
func NewNotFound(msg string) error { return &notFoundError{msg: msg} }

// IsNotFound 报告 err 是否为 S3 的"资源不存在"。
//
// 覆盖两种语义, 调用方需按场景使用:
//   - 对象 / 桶不存在 (NoSuchBucket / NoSuchKey / 404)
//   - 子资源配置未设置 (NoSuchBucketPolicy / NoSuchLifecycleConfiguration 等,
//     服务端同样以 404 表达)
func IsNotFound(err error) bool {
	apiErr := ErrorOf(err)
	if apiErr == nil {
		return false
	}
	return apiErr.StatusCode == http.StatusNotFound || strings.Contains(apiErr.Code, "NoSuch")
}

// IsAccessDenied 报告 err 是否为 S3 的"无权限"。
func IsAccessDenied(err error) bool {
	apiErr := ErrorOf(err)
	if apiErr == nil {
		return false
	}
	return apiErr.StatusCode == http.StatusForbidden || strings.Contains(apiErr.Code, "AccessDenied")
}

// HasCode 报告 err 链上是否存在 Code 恰好等于 code 的 S3 错误。
func HasCode(err error, code string) bool {
	apiErr := ErrorOf(err)
	return apiErr != nil && apiErr.Code == code
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
