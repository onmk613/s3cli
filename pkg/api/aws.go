package api

import (
	"context"
	"errors"
	"io"
	"net/url"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// ============ 数据结构定义 ============

type BucketInfo struct {
	Name         string
	CreationDate time.Time
}

type ObjectInfo struct {
	Key          string
	Size         int64
	ETag         string
	LastModified time.Time
	ContentType  string
	Metadata     map[string]string
	StorageClass string
}

type PutOption func(*putOptions)

type putOptions struct {
	ContentType   string
	ContentLength int64
	Metadata      map[string]string
	StorageClass  string
}

func WithContentType(ct string) PutOption {
	return func(o *putOptions) { o.ContentType = ct }
}

func WithContentLength(length int64) PutOption {
	return func(o *putOptions) { o.ContentLength = length }
}

func WithMetadata(meta map[string]string) PutOption {
	return func(o *putOptions) { o.Metadata = meta }
}

func WithStorageClass(sc string) PutOption {
	return func(o *putOptions) { o.StorageClass = sc }
}

type ListOption func(*listOptions)

type listOptions struct {
	Delimiter         string
	MaxKeys           int32
	ContinuationToken string
}

func WithDelimiter(delimiter string) ListOption {
	return func(o *listOptions) { o.Delimiter = delimiter }
}

func WithMaxKeys(max int32) ListOption {
	return func(o *listOptions) { o.MaxKeys = max }
}

func WithContinuationToken(token string) ListOption {
	return func(o *listOptions) { o.ContinuationToken = token }
}

type ListObjectsResult struct {
	Objects        []ObjectInfo
	CommonPrefixes []string // 目录（配合 Delimiter 使用）
	IsTruncated    bool
	NextToken      string
}

type DeleteError struct {
	Key     string
	Code    string
	Message string
}

type CompletedPart struct {
	PartNumber int32
	ETag       string
}

type PartInfo struct {
	PartNumber   int32
	ETag         string
	Size         int64
	LastModified time.Time
}

// ============ S3GenericAPI 实现 ============

type S3GenericAPI struct {
	S3 *s3.Client
}

// ---------- Bucket 操作 ----------

func (c *S3GenericAPI) CreateBucket(ctx context.Context, bucket string) error {
	_, err := c.S3.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})
	return err
}

func (c *S3GenericAPI) DeleteBucket(ctx context.Context, bucket string) error {
	_, err := c.S3.DeleteBucket(ctx, &s3.DeleteBucketInput{
		Bucket: aws.String(bucket),
	})
	return err
}

func (c *S3GenericAPI) HeadBucket(ctx context.Context, bucket string) (bool, error) {
	_, err := c.S3.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		var notFound *types.NotFound
		if errors.As(err, &notFound) {
			return false, nil
		}
		// 兼容部分厂商返回的不同错误类型
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) {
			switch apiErr.ErrorCode() {
			case "NotFound", "NoSuchBucket", "404":
				return false, nil
			}
		}
		return false, err
	}
	return true, nil
}

func (c *S3GenericAPI) ListBuckets(ctx context.Context) ([]BucketInfo, error) {
	out, err := c.S3.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return nil, err
	}
	buckets := make([]BucketInfo, 0, len(out.Buckets))
	for _, b := range out.Buckets {
		info := BucketInfo{Name: aws.ToString(b.Name)}
		if b.CreationDate != nil {
			info.CreationDate = *b.CreationDate
		}
		buckets = append(buckets, info)
	}
	return buckets, nil
}

// ---------- Object 基础操作 ----------

func (c *S3GenericAPI) PutObject(ctx context.Context, bucket, key string, body io.Reader, opts ...PutOption) error {
	o := &putOptions{}
	for _, opt := range opts {
		opt(o)
	}

	input := &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   body,
	}
	if o.ContentType != "" {
		input.ContentType = aws.String(o.ContentType)
	}
	if o.ContentLength > 0 {
		input.ContentLength = aws.Int64(o.ContentLength)
	}
	if len(o.Metadata) > 0 {
		input.Metadata = o.Metadata
	}
	if o.StorageClass != "" {
		input.StorageClass = types.StorageClass(o.StorageClass)
	}

	_, err := c.S3.PutObject(ctx, input)
	return err
}

func (c *S3GenericAPI) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, *ObjectInfo, error) {
	out, err := c.S3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, nil, err
	}

	info := &ObjectInfo{
		Key:         key,
		Size:        aws.ToInt64(out.ContentLength),
		ETag:        aws.ToString(out.ETag),
		ContentType: aws.ToString(out.ContentType),
		Metadata:    out.Metadata,
	}
	if out.LastModified != nil {
		info.LastModified = *out.LastModified
	}
	return out.Body, info, nil
}

func (c *S3GenericAPI) DeleteObject(ctx context.Context, bucket, key string) error {
	_, err := c.S3.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	return err
}

func (c *S3GenericAPI) DeleteObjects(ctx context.Context, bucket string, keys []string) ([]DeleteError, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	objects := make([]types.ObjectIdentifier, 0, len(keys))
	for _, k := range keys {
		objects = append(objects, types.ObjectIdentifier{Key: aws.String(k)})
	}

	out, err := c.S3.DeleteObjects(ctx, &s3.DeleteObjectsInput{
		Bucket: aws.String(bucket),
		Delete: &types.Delete{
			Objects: objects,
			Quiet:   aws.Bool(true), // 只返回错误，成功不返回
		},
	})
	if err != nil {
		return nil, err
	}

	delErrs := make([]DeleteError, 0, len(out.Errors))
	for _, e := range out.Errors {
		delErrs = append(delErrs, DeleteError{
			Key:     aws.ToString(e.Key),
			Code:    aws.ToString(e.Code),
			Message: aws.ToString(e.Message),
		})
	}
	return delErrs, nil
}

func (c *S3GenericAPI) HeadObject(ctx context.Context, bucket, key string) (*ObjectInfo, error) {
	out, err := c.S3.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}

	info := &ObjectInfo{
		Key:          key,
		Size:         aws.ToInt64(out.ContentLength),
		ETag:         aws.ToString(out.ETag),
		ContentType:  aws.ToString(out.ContentType),
		Metadata:     out.Metadata,
		StorageClass: string(out.StorageClass),
	}
	if out.LastModified != nil {
		info.LastModified = *out.LastModified
	}
	return info, nil
}

func (c *S3GenericAPI) CopyObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) error {
	// CopySource 格式: /bucket/key，需要 URL 编码
	source := url.PathEscape(srcBucket + "/" + srcKey)
	_, err := c.S3.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(dstBucket),
		Key:        aws.String(dstKey),
		CopySource: aws.String(source),
	})
	return err
}

// ---------- Object 列举 ----------

func (c *S3GenericAPI) ListObjects(ctx context.Context, bucket, prefix string, opts ...ListOption) (*ListObjectsResult, error) {
	o := &listOptions{MaxKeys: 1000}
	for _, opt := range opts {
		opt(o)
	}

	input := &s3.ListObjectsV2Input{
		Bucket:  aws.String(bucket),
		Prefix:  aws.String(prefix),
		MaxKeys: aws.Int32(o.MaxKeys),
	}
	if o.Delimiter != "" {
		input.Delimiter = aws.String(o.Delimiter)
	}
	if o.ContinuationToken != "" {
		input.ContinuationToken = aws.String(o.ContinuationToken)
	}

	out, err := c.S3.ListObjectsV2(ctx, input)
	if err != nil {
		return nil, err
	}

	result := &ListObjectsResult{
		IsTruncated: aws.ToBool(out.IsTruncated),
		NextToken:   aws.ToString(out.NextContinuationToken),
	}
	for _, obj := range out.Contents {
		info := ObjectInfo{
			Key:          aws.ToString(obj.Key),
			Size:         aws.ToInt64(obj.Size),
			ETag:         aws.ToString(obj.ETag),
			StorageClass: string(obj.StorageClass),
		}
		if obj.LastModified != nil {
			info.LastModified = *obj.LastModified
		}
		result.Objects = append(result.Objects, info)
	}
	for _, cp := range out.CommonPrefixes {
		result.CommonPrefixes = append(result.CommonPrefixes, aws.ToString(cp.Prefix))
	}
	return result, nil
}

// ---------- 分片上传 ----------

func (c *S3GenericAPI) CreateMultipartUpload(ctx context.Context, bucket, key string) (string, error) {
	out, err := c.S3.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return "", err
	}
	return aws.ToString(out.UploadId), nil
}

func (c *S3GenericAPI) UploadPart(ctx context.Context, bucket, key, uploadID string, partNumber int32, body io.Reader) (string, error) {
	out, err := c.S3.UploadPart(ctx, &s3.UploadPartInput{
		Bucket:     aws.String(bucket),
		Key:        aws.String(key),
		UploadId:   aws.String(uploadID),
		PartNumber: aws.Int32(partNumber),
		Body:       body,
	})
	if err != nil {
		return "", err
	}
	return aws.ToString(out.ETag), nil
}

func (c *S3GenericAPI) CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string, parts []CompletedPart) error {
	completed := make([]types.CompletedPart, 0, len(parts))
	for _, p := range parts {
		completed = append(completed, types.CompletedPart{
			PartNumber: aws.Int32(p.PartNumber),
			ETag:       aws.String(p.ETag),
		})
	}

	_, err := c.S3.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: completed,
		},
	})
	return err
}

func (c *S3GenericAPI) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	_, err := c.S3.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
	})
	return err
}

func (c *S3GenericAPI) ListParts(ctx context.Context, bucket, key, uploadID string) ([]PartInfo, error) {
	out, err := c.S3.ListParts(ctx, &s3.ListPartsInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
	})
	if err != nil {
		return nil, err
	}

	parts := make([]PartInfo, 0, len(out.Parts))
	for _, p := range out.Parts {
		info := PartInfo{
			PartNumber: aws.ToInt32(p.PartNumber),
			ETag:       aws.ToString(p.ETag),
			Size:       aws.ToInt64(p.Size),
		}
		if p.LastModified != nil {
			info.LastModified = *p.LastModified
		}
		parts = append(parts, info)
	}
	return parts, nil
}

// ---------- 预签名 URL ----------

func (c *S3GenericAPI) PresignGetObject(ctx context.Context, bucket, key string, expires time.Duration) (string, error) {
	presignClient := s3.NewPresignClient(c.S3)
	req, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(expires))
	if err != nil {
		return "", err
	}
	return req.URL, nil
}

func (c *S3GenericAPI) PresignPutObject(ctx context.Context, bucket, key string, expires time.Duration) (string, error) {
	presignClient := s3.NewPresignClient(c.S3)
	req, err := presignClient.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(expires))
	if err != nil {
		return "", err
	}
	return req.URL, nil
}

func (c *S3GenericAPI) PutBucketPolicy(ctx context.Context, bucket string, data []byte) error {
	_, err := c.S3.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{
		Bucket: aws.String(bucket),
		Policy: aws.String(string(data)),
	})
	return err
}

func (c *S3GenericAPI) GetBucketPolicy(ctx context.Context, bucket string) (string, error) {
	out, err := c.S3.GetBucketPolicy(ctx, &s3.GetBucketPolicyInput{Bucket: aws.String(bucket)})
	if err != nil {
		return "", err
	}
	return aws.ToString(out.Policy), nil
}

func (c *S3GenericAPI) DeleteBucketPolicy(ctx context.Context, bucket string) error {
	_, err := c.S3.DeleteBucketPolicy(ctx, &s3.DeleteBucketPolicyInput{Bucket: aws.String(bucket)})
	return err
}
