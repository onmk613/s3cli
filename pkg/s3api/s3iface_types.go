//go:build !aws

// s3iface_types.go 将 s3api 的对外 DTO 类型别名到中立的 s3iface 包.
//
// s3iface 拥有类型定义; s3api 通过别名引用, 使得:
//   - s3api.Client 的方法签名使用 s3iface 类型, 从而结构化满足 s3iface.S3Operations 接口
//   - 上层代码 (internal/action) 直接用 s3iface 类型, 不依赖具体后端
//
// 仅为类型别名, 不含任何逻辑. 所有 HTTP/签名/XML 逻辑留在各自实现文件中.

package s3api

import "s3cli/pkg/s3iface"

// ---- 对象列举 ----
type ObjectInfo = s3iface.ObjectInfo
type ListObjectsV2Options = s3iface.ListObjectsV2Options
type ListObjectsV2Output = s3iface.ListObjectsV2Output
type ListObjectVersionsOptions = s3iface.ListObjectVersionsOptions
type ListObjectVersionsOutput = s3iface.ListObjectVersionsOutput
type BucketInfo = s3iface.BucketInfo
type MakeBucketOptions = s3iface.MakeBucketOptions

// 包内别名 (原为未导出类型, 用于 XML 解码与跨方法传递)
type owner = s3iface.Owner
type objectVersion = s3iface.ObjectVersion
type deleteMarker = s3iface.DeleteMarker

// ---- 对象操作 ----
type HeadObjectOutput = s3iface.HeadObjectOutput
type GetObjectOptions = s3iface.GetObjectOptions
type GetObjectOutput = s3iface.GetObjectOutput
type PutObjectOptions = s3iface.PutObjectOptions
type PutObjectOutput = s3iface.PutObjectOutput
type CopyObjectOptions = s3iface.CopyObjectOptions
type CopyObjectOutput = s3iface.CopyObjectOutput
type DeleteObjectOutput = s3iface.DeleteObjectOutput
type ObjectIdentifier = s3iface.ObjectIdentifier
type DeleteObjectsOutput = s3iface.DeleteObjectsOutput
type DeletedObject = s3iface.DeletedObject
type DeleteObjectError = s3iface.DeleteObjectError

// ---- 分片上传 ----
type CreateMultipartUploadOutput = s3iface.CreateMultipartUploadOutput
type UploadPartOutput = s3iface.UploadPartOutput
type CompletedPart = s3iface.CompletedPart
type CompleteMultipartUploadOutput = s3iface.CompleteMultipartUploadOutput
type ListMultipartUploadsOptions = s3iface.ListMultipartUploadsOptions
type ListMultipartUploadsOutput = s3iface.ListMultipartUploadsOutput
type ListPartsOutput = s3iface.ListPartsOutput

// 包内别名
type uploadInfo = s3iface.UploadInfo
type partInfo = s3iface.PartInfo

// ---- 预签名 / 错误 ----
type PresignOptions = s3iface.PresignOptions
type ErrorResponse = s3iface.ErrorResponse

// ---- 桶子资源配置 ----
type CorsConfig = s3iface.CorsConfig
type CorsRule = s3iface.CorsRule
type ServerSideEncryptionByDefault = s3iface.ServerSideEncryptionByDefault
type ServerSideEncryptionRule = s3iface.ServerSideEncryptionRule
type ServerSideEncryptionConfiguration = s3iface.ServerSideEncryptionConfiguration
type LifecycleConfig = s3iface.LifecycleConfig
type LifecycleRule = s3iface.LifecycleRule
type Filter = s3iface.Filter
type Tag = s3iface.Tag
type And = s3iface.And
type Transition = s3iface.Transition
type Expiration = s3iface.Expiration
type NoncurrentVersionExpiration = s3iface.NoncurrentVersionExpiration
type NoncurrentVersionTransition = s3iface.NoncurrentVersionTransition
type AbortIncompleteMultipartUpload = s3iface.AbortIncompleteMultipartUpload
type TopicConfiguration = s3iface.TopicConfiguration
type QueueConfiguration = s3iface.QueueConfiguration
type LambdaFunctionConfiguration = s3iface.LambdaFunctionConfiguration
type NotificationFilter = s3iface.NotificationFilter
type FilterRule = s3iface.FilterRule
type NotificationConfiguration = s3iface.NotificationConfiguration
type Tagging = s3iface.Tagging
type BucketVersioningStatus = s3iface.BucketVersioningStatus

// 版本控制状态常量别名
const (
	VersioningEnabled   = s3iface.VersioningEnabled
	VersioningSuspended = s3iface.VersioningSuspended
)

// ParseBucketCorsConfig / ParseBucketLifecycleConfig 转发到 s3iface (向后兼容).
var (
	ParseBucketCorsConfig      = s3iface.ParseBucketCorsConfig
	ParseBucketLifecycleConfig = s3iface.ParseBucketLifecycleConfig
)
