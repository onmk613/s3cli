// s3iface.go 定义 S3 操作的中立接口 S3Operations 及分页器接口.
//
// S3Operations 涵盖桶管理、桶子资源配置、对象操作、分片上传、预签名等全部底层操作.
// api.Client (自建 HTTP + SigV4) 实现此接口.

package s3iface

import (
	"context"
	"io"
)

// S3Operations 抽象所有 S3 底层操作, 由自建请求客户端 api.Client 实现.
type S3Operations interface {

	// ---- 桶基础操作 ----
	ListBuckets(ctx context.Context) ([]BucketInfo, error)
	CreateBucket(ctx context.Context, bucketName string, opts *MakeBucketOptions) error
	DeleteBucket(ctx context.Context, bucketName string) error
	GetBucketLocation(ctx context.Context, bucket string) (string, error)

	// ---- 桶子资源: CORS ----
	SetBucketCors(ctx context.Context, bucket string, config *CorsConfig) error
	GetBucketCors(ctx context.Context, bucket string) (*CorsConfig, error)
	DeleteBucketCors(ctx context.Context, bucket string) error

	// ---- 桶子资源: 加密 ----
	SetBucketEncryption(ctx context.Context, bucket string, config *ServerSideEncryptionConfiguration) error
	GetBucketEncryption(ctx context.Context, bucket string) (*ServerSideEncryptionConfiguration, error)
	DeleteBucketEncryption(ctx context.Context, bucket string) error

	// ---- 桶子资源: 生命周期 ----
	SetBucketLifecycle(ctx context.Context, bucket string, config *LifecycleConfig) error
	GetBucketLifecycle(ctx context.Context, bucket string) (*LifecycleConfig, error)
	DeleteBucketLifecycle(ctx context.Context, bucket string) error

	// ---- 桶子资源: 事件通知 ----
	SetBucketNotification(ctx context.Context, bucket string, config *NotificationConfiguration) error
	GetBucketNotification(ctx context.Context, bucket string) (*NotificationConfiguration, error)
	DeleteBucketNotification(ctx context.Context, bucket string) error

	// ---- 桶子资源: 标签 ----
	SetBucketTagging(ctx context.Context, bucket string, tags []Tagging) error
	GetBucketTagging(ctx context.Context, bucket string) ([]Tagging, error)
	DeleteBucketTagging(ctx context.Context, bucket string) error

	// ---- 桶子资源: 版本控制 ----
	SetBucketVersioning(ctx context.Context, bucket string, status BucketVersioningStatus) error
	GetBucketVersioning(ctx context.Context, bucket string) (BucketVersioningStatus, error)

	// ---- 桶子资源: 公共访问阻断 ----
	GetPublicAccessBlock(ctx context.Context, bucket string) (*PublicAccessBlockConfiguration, error)
	PutPublicAccessBlock(ctx context.Context, bucket string, config *PublicAccessBlockConfiguration) error
	DeletePublicAccessBlock(ctx context.Context, bucket string) error

	// ---- 桶子资源: Object Lock 配置 ----
	GetObjectLockConfiguration(ctx context.Context, bucket string) (*ObjectLockConfiguration, error)
	PutObjectLockConfiguration(ctx context.Context, bucket string, config *ObjectLockConfiguration) error

	// ---- 桶子资源: 复制 (Replication) ----
	GetBucketReplication(ctx context.Context, bucket string) (*ReplicationConfiguration, error)
	PutBucketReplication(ctx context.Context, bucket string, config *ReplicationConfiguration) error
	DeleteBucketReplication(ctx context.Context, bucket string) error

	// ---- 桶 / 对象 ACL (canned + grant 头; GET 返回原始 XML) ----
	GetBucketACL(ctx context.Context, bucket string) ([]byte, error)
	PutBucketACL(ctx context.Context, bucket string, opts *ACLOptions) error
	GetObjectACL(ctx context.Context, bucket, key, versionID string) ([]byte, error)
	PutObjectACL(ctx context.Context, bucket, key, versionID string, opts *ACLOptions) error

	// ---- Object Lock 对象级 (retention / legal-hold) ----
	GetObjectRetention(ctx context.Context, bucket, key, versionID string) (*ObjectLockRetention, error)
	PutObjectRetention(ctx context.Context, bucket, key, versionID string, retention *ObjectLockRetention) error
	GetObjectLegalHold(ctx context.Context, bucket, key, versionID string) (*ObjectLockLegalHold, error)
	PutObjectLegalHold(ctx context.Context, bucket, key, versionID string, hold *ObjectLockLegalHold) error

	// ---- 归档恢复 (Glacier) ----
	RestoreObject(ctx context.Context, bucket, key, versionID string, req *RestoreRequest) error

	// ---- 桶子资源: 策略 ----
	SetBucketPolicy(ctx context.Context, bucket string, data []byte) error
	GetBucketPolicy(ctx context.Context, bucket string) ([]byte, error)
	DeleteBucketPolicy(ctx context.Context, bucket string) error

	// ---- 对象列举 ----
	ListObjectsV2(ctx context.Context, bucket string, opts *ListObjectsV2Options) (*ListObjectsV2Output, error)
	ListObjectVersions(ctx context.Context, bucket string, opts *ListObjectVersionsOptions) (*ListObjectVersionsOutput, error)

	// ---- 对象元数据 / 下载 / 上传 / 复制 / 删除 ----
	HeadObject(ctx context.Context, bucket, key, versionID string) (*HeadObjectOutput, error)
	GetObject(ctx context.Context, bucket, key string, opts *GetObjectOptions) (*GetObjectOutput, error)
	PutObject(ctx context.Context, bucket, key string, body []byte, opts *PutObjectOptions) (*PutObjectOutput, error)
	PutObjectStream(ctx context.Context, bucket, key string, body io.ReadSeeker, contentLength int64, opts *PutObjectOptions) (*PutObjectOutput, error)
	CopyObject(ctx context.Context, srcBucket, srcKey, destBucket, destKey string, opts *CopyObjectOptions) (*CopyObjectOutput, error)
	DeleteObject(ctx context.Context, bucket, key, versionID string) (*DeleteObjectOutput, error)
	DeleteObjects(ctx context.Context, bucket string, objects []ObjectIdentifier, quiet bool) (*DeleteObjectsOutput, error)

	// ---- 对象标签 ----
	SetObjectTagging(ctx context.Context, bucket, key string, tags []Tagging, versionID string) error
	GetObjectTagging(ctx context.Context, bucket, key, versionID string) ([]Tagging, error)
	DeleteObjectTagging(ctx context.Context, bucket, key, versionID string) error

	// ---- S3 Select ----
	SelectObjectContent(ctx context.Context, bucket, key string, input *SelectObjectInput, onRecord func(payload []byte) error) (*SelectObjectStats, error)

	// ---- 分片上传 ----
	CreateMultipartUpload(ctx context.Context, bucket, key string, opts *PutObjectOptions) (*CreateMultipartUploadOutput, error)
	UploadPart(ctx context.Context, bucket, key, uploadID string, partNumber int, body []byte) (*UploadPartOutput, error)
	UploadPartCopy(ctx context.Context, srcBucket, srcKey, destBucket, destKey, uploadID string, partNumber int, opts *UploadPartCopyOptions) (*UploadPartCopyOutput, error)
	CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string, parts []CompletedPart) (*CompleteMultipartUploadOutput, error)
	AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error
	ListMultipartUploads(ctx context.Context, bucket string, opts *ListMultipartUploadsOptions) (*ListMultipartUploadsOutput, error)
	ListParts(ctx context.Context, bucket, key, uploadID string, partNumberMarker, maxParts int) (*ListPartsOutput, error)

	// ---- 预签名 URL ----
	PresignedURL(ctx context.Context, bucket, key string, opts *PresignOptions) (string, error)
	PresignV2(ctx context.Context, bucket, key, method string, expires int64) (string, error)

	// ---- 分页器工厂 ----
	NewListObjectsV2Paginator(bucket string, opts *ListObjectsV2Options) ListObjectsV2Paginator
	NewListObjectVersionsPaginator(bucket string, opts *ListObjectVersionsOptions) ListObjectVersionsPaginator

	// ---- 客户端元数据 (用于 mirror/diff 判断同 endpoint, 非标准 S3 操作) ----
	AccessKey() string
	SecretKey() string
	SessionToken() string
	Endpoint() string
}

// ListObjectsV2Paginator 封装 ListObjectsV2 自动分页逻辑.
type ListObjectsV2Paginator interface {
	HasMorePages() bool
	NextPage(ctx context.Context) (*ListObjectsV2Output, error)
}

// ListObjectVersionsPaginator 封装 ListObjectVersions 自动分页逻辑.
type ListObjectVersionsPaginator interface {
	HasMorePages() bool
	NextPage(ctx context.Context) (*ListObjectVersionsOutput, error)
}
