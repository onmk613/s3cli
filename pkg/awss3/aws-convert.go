// aws-convert.go 提供 AWS SDK 类型 ↔ s3iface DTO 类型的双向转换, 以及 SDK 错误适配.
//
// sdkErr 将 SDK 的 smithy/awshttp 错误统一包装为 *s3iface.ErrorResponse,
// 使上层 (internal/action) 的错误处理在两个后端间保持一致.

package awss3

import (
	"errors"
	"fmt"

	"s3cli/pkg/s3iface"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// sdkErr 将 SDK 错误适配为 *s3iface.ErrorResponse (若包含结构化信息).
// 无法识别的错误原样返回.
func sdkErr(err error) error {
	if err == nil {
		return nil
	}
	var respErr *awshttp.ResponseError
	if errors.As(err, &respErr) {
		e := &s3iface.ErrorResponse{
			StatusCode: respErr.HTTPStatusCode(),
		}
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) {
			e.Code = apiErr.ErrorCode()
			e.Message = apiErr.ErrorMessage()
		} else {
			e.Code = fmt.Sprintf("HTTP %d", respErr.HTTPStatusCode())
			e.Message = respErr.Error()
		}
		return e
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return &s3iface.ErrorResponse{
			Code:    apiErr.ErrorCode(),
			Message: apiErr.ErrorMessage(),
		}
	}
	return err
}

// ---- 对象列举转换 ----

func toObjectInfo(o types.Object) s3iface.ObjectInfo {
	info := s3iface.ObjectInfo{
		Key:          aws.ToString(o.Key),
		LastModified: aws.ToTime(o.LastModified),
		ETag:         aws.ToString(o.ETag),
		Size:         aws.ToInt64(o.Size),
		StorageClass: string(o.StorageClass),
	}
	if o.Owner != nil {
		info.Owner = &s3iface.Owner{
			ID:          aws.ToString(o.Owner.ID),
			DisplayName: aws.ToString(o.Owner.DisplayName),
		}
	}
	return info
}

func toObjectVersion(v types.ObjectVersion) s3iface.ObjectVersion {
	out := s3iface.ObjectVersion{
		IsLatest:     aws.ToBool(v.IsLatest),
		VersionID:    aws.ToString(v.VersionId),
		Key:          aws.ToString(v.Key),
		LastModified: aws.ToTime(v.LastModified),
		ETag:         aws.ToString(v.ETag),
		Size:         aws.ToInt64(v.Size),
		StorageClass: string(v.StorageClass),
	}
	if v.Owner != nil {
		out.Owner = &s3iface.Owner{
			ID:          aws.ToString(v.Owner.ID),
			DisplayName: aws.ToString(v.Owner.DisplayName),
		}
	}
	return out
}

func toDeleteMarker(m types.DeleteMarkerEntry) s3iface.DeleteMarker {
	out := s3iface.DeleteMarker{
		IsLatest:     aws.ToBool(m.IsLatest),
		VersionID:    aws.ToString(m.VersionId),
		Key:          aws.ToString(m.Key),
		LastModified: aws.ToTime(m.LastModified),
	}
	if m.Owner != nil {
		out.Owner = &s3iface.Owner{
			ID:          aws.ToString(m.Owner.ID),
			DisplayName: aws.ToString(m.Owner.DisplayName),
		}
	}
	return out
}

func toUploadInfo(u types.MultipartUpload) s3iface.UploadInfo {
	return s3iface.UploadInfo{
		Key:          aws.ToString(u.Key),
		UploadID:     aws.ToString(u.UploadId),
		Initiated:    aws.ToTime(u.Initiated),
		StorageClass: string(u.StorageClass),
	}
}

func toPartInfo(p types.Part) s3iface.PartInfo {
	return s3iface.PartInfo{
		PartNumber:   int(aws.ToInt32(p.PartNumber)),
		LastModified: aws.ToTime(p.LastModified),
		ETag:         aws.ToString(p.ETag),
		Size:         aws.ToInt64(p.Size),
	}
}

func toBucketInfo(b types.Bucket) s3iface.BucketInfo {
	return s3iface.BucketInfo{
		Name:         aws.ToString(b.Name),
		CreationDate: aws.ToTime(b.CreationDate),
	}
}
