package api

import (
	"context"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// NewS3GenericAPI 通用接口实现, 主要处理所有对象存储厂商都兼容的s3 api协议
func NewS3GenericAPI(client *s3.Client) GenericAPI {
	return &S3GenericAPI{
		S3: client,
	}
}

// GenericAPI 所有对象存储厂商都兼容的 S3 标准操作
type GenericAPI interface {
	// ===== Bucket 操作 =====
	CreateBucket(ctx context.Context, bucket string) error
	DeleteBucket(ctx context.Context, bucket string) error
	HeadBucket(ctx context.Context, bucket string) (bool, error)
	ListBuckets(ctx context.Context) ([]BucketInfo, error)

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

	// bucket policy
	PutBucketPolicy(ctx context.Context, bucket string, data []byte) error
	GetBucketPolicy(ctx context.Context, bucket string) (string, error)
	DeleteBucketPolicy(ctx context.Context, bucket string) error
}
