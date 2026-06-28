package api

import (
	"context"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// GenericAPI 实现标准 S3 API
type GenericAPI interface {
	// ===== Bucket Management =====
	CreateBucket(ctx context.Context, bucket string) error
	DeleteBucket(ctx context.Context, bucket string) error
	ListBuckets(ctx context.Context) ([]BucketInfo, error)

	// ===== Bucket Configuration =====
	SetBucketPolicy(ctx context.Context, bucket string, data []byte) error
	GetBucketPolicy(ctx context.Context, bucket string) (string, error)
	DeleteBucketPolicy(ctx context.Context, bucket string) error
	SetBucketCors(ctx context.Context, bucket string, data []byte) error
	GetBucketCors(ctx context.Context, bucket string) ([]byte, error)
	DeleteBucketCors(ctx context.Context, bucket string) error
	SetBucketVersioning(ctx context.Context, bucket, status string) error
	GetBucketVersioning(ctx context.Context, bucket string) (string, error)

	// Object Tagging
	SetTagging(ctx context.Context, bucket, prefix string, tagStr map[string]string) error
	GetTagging(ctx context.Context, bucket, prefix string) (map[string]string, error)
	DelTagging(ctx context.Context, bucket, prefix string) error

	// ===== Object 基础操作 =====
	PutObject(ctx context.Context, bucket, key string, body io.Reader, opts ...PutOption) error
	GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, *ObjectInfo, error)
	DeleteObject(ctx context.Context, bucket, key string) error
	DeleteObjects(ctx context.Context, bucket string, keys []string) ([]DeleteError, error)
	HeadObject(ctx context.Context, bucket, key string) (*ObjectInfo, error)
	CopyObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) error

	// ===== Object 列举 =====
	ListObjects(ctx context.Context, bucket, prefix string, opts ...ListOption) (*ListObjectsResult, error)

	// ===== 分片上传 (Multipart Upload) =====
	CreateMultipartUpload(ctx context.Context, bucket, key string) (uploadID string, err error)
	UploadPart(ctx context.Context, bucket, key, uploadID string, partNumber int32, body io.Reader) (etag string, err error)
	CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string, parts []CompletedPart) error
	AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error
	ListParts(ctx context.Context, bucket, key, uploadID string) ([]PartInfo, error)

	// ===== 预签名 URL (Presigned URL) =====
	PresignGetObject(ctx context.Context, bucket, key string, expires time.Duration) (string, error)
	PresignPutObject(ctx context.Context, bucket, key string, expires time.Duration) (string, error)
}

// NewS3GenericAPI 创建 aws sdk 的 API
func NewS3GenericAPI(client *s3.Client) GenericAPI {
	return &S3GenericAPI{
		S3: client,
	}
}

// LifecycleAPI 实现 Bucket Lifecycle
type LifecycleAPI interface {
	SetLifecycle(ctx context.Context, bucket string, data []byte) error
	GetLifecycle(ctx context.Context, bucket string) ([]byte, error)
	DelLifecycle(ctx context.Context, bucket string) error
}

// NewS3LifecycleAPI 创建 aws sdk 的 API
func NewS3LifecycleAPI(client *s3.Client) LifecycleAPI {
	return &S3GenericAPI{
		S3: client,
	}
}

// EventAPI 实现 Event/Notification
type EventAPI interface {
	SetEvent(ctx context.Context, bucket string, data []byte) error
	GetEvent(ctx context.Context, bucket string) ([]byte, error)
	DelEvent(ctx context.Context, bucket string) error
}

// NewS3EventAPI 创建 aws sdk 的 API
func NewS3EventAPI(client *s3.Client) EventAPI {
	return &S3GenericAPI{
		S3: client,
	}
}

type IAMPolicyAPI interface{}
type ClusterInfoAPI interface{}
type UserAPI interface{}
type AclAPI interface{}
type Encryption interface{}
type QuotaAPI interface{}
type MetadataAPI interface{}
type StorageAPI interface{}
