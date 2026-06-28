package api

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type S3GenericAPI struct {
	S3 *s3.Client
}

// ===== Bucket Management and Configuration =====

// Bucket Action
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

// Bucket Policy
func (c *S3GenericAPI) SetBucketPolicy(ctx context.Context, bucket string, data []byte) error {
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

// Bucket CORS
func (c *S3GenericAPI) SetBucketCors(ctx context.Context, bucket string, data []byte) error {
	var x xmlCORSConfiguration
	if err := xml.Unmarshal(data, &x); err != nil {
		return err
	}
	xmlData := &s3types.CORSConfiguration{CORSRules: make([]s3types.CORSRule, 0, len(x.Rules))}
	for _, r := range x.Rules {
		rule := s3types.CORSRule{
			AllowedMethods: r.AllowedMethods,
			AllowedOrigins: r.AllowedOrigins,
			AllowedHeaders: r.AllowedHeaders,
			ExposeHeaders:  r.ExposeHeaders,
			MaxAgeSeconds:  r.MaxAgeSeconds,
		}
		if r.ID != "" {
			rule.ID = aws.String(r.ID)
		}
		xmlData.CORSRules = append(xmlData.CORSRules, rule)
	}
	_, err := c.S3.PutBucketCors(ctx, &s3.PutBucketCorsInput{
		Bucket:            aws.String(bucket),
		CORSConfiguration: xmlData,
	})
	return err
}

func (c *S3GenericAPI) GetBucketCors(ctx context.Context, bucket string) ([]byte, error) {
	out, err := c.S3.GetBucketCors(ctx, &s3.GetBucketCorsInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(map[string]any{"CORSRules": out.CORSRules}, "", "  ")
}

func (c *S3GenericAPI) DeleteBucketCors(ctx context.Context, bucket string) error {
	_, err := c.S3.DeleteBucketCors(ctx, &s3.DeleteBucketCorsInput{
		Bucket: aws.String(bucket),
	})
	return err
}

// Bucket Lifecycle
func (c *S3GenericAPI) SetLifecycle(ctx context.Context, bucket string, data []byte) error {
	var cfg s3types.BucketLifecycleConfiguration
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse json: %w", err)
	}
	//if len(cfg.Rules) == 0 {
	//	return errors.New("no lifecycle rules found")
	//}
	//for i, r := range cfg.Rules {
	//	if r.Status == "" {
	//		return fmt.Errorf("rule[%d] missing required field 'Status' (Enabled/Disabled)", i)
	//	}
	//	if r.Filter == nil {
	//		return fmt.Errorf("rule[%d] must specify 'Filter' or legacy 'Prefix'", i)
	//	}
	//}
	_, err := c.S3.PutBucketLifecycleConfiguration(ctx, &s3.PutBucketLifecycleConfigurationInput{
		Bucket:                 aws.String(bucket),
		LifecycleConfiguration: &cfg,
	})
	return err
}

func (c *S3GenericAPI) GetLifecycle(ctx context.Context, bucket string) ([]byte, error) {
	out, err := c.S3.GetBucketLifecycleConfiguration(ctx,
		&s3.GetBucketLifecycleConfigurationInput{
			Bucket: aws.String(bucket)})
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(map[string]any{"Rules": out.Rules}, "", "  ")
}

func (c *S3GenericAPI) DelLifecycle(ctx context.Context, bucket string) error {
	_, err := c.S3.DeleteBucketLifecycle(ctx,
		&s3.DeleteBucketLifecycleInput{
			Bucket: aws.String(bucket)})
	return err
}

// Bucket Notification
func (c *S3GenericAPI) SetEvent(ctx context.Context, bucket string, data []byte) error {
	var cfg s3types.NotificationConfiguration
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse json: %w", err)
	}
	_, err := c.S3.PutBucketNotificationConfiguration(ctx, &s3.PutBucketNotificationConfigurationInput{
		Bucket:                    aws.String(bucket),
		NotificationConfiguration: &cfg,
	})
	return err
}

func (c *S3GenericAPI) GetEvent(ctx context.Context, bucket string) ([]byte, error) {
	out, err := c.S3.GetBucketNotificationConfiguration(ctx, &s3.GetBucketNotificationConfigurationInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(out, "", "  ")
}

func (c *S3GenericAPI) DelEvent(ctx context.Context, bucket string) error {
	_, err := c.S3.PutBucketNotificationConfiguration(ctx, &s3.PutBucketNotificationConfigurationInput{
		Bucket:                    aws.String(bucket),
		NotificationConfiguration: &s3types.NotificationConfiguration{},
	})
	return err
}

// Bucket Versioning
func (c *S3GenericAPI) SetBucketVersioning(ctx context.Context, bucket, status string) error {
	_, err := c.S3.PutBucketVersioning(ctx,
		&s3.PutBucketVersioningInput{
			Bucket: aws.String(bucket),
			VersioningConfiguration: &s3types.VersioningConfiguration{
				Status: s3types.BucketVersioningStatus(status)}})
	return err
}

func (c *S3GenericAPI) GetBucketVersioning(ctx context.Context, bucket string) (string, error) {
	out, err := c.S3.GetBucketVersioning(ctx,
		&s3.GetBucketVersioningInput{Bucket: aws.String(bucket)})
	return string(out.Status), err
}

// Bucket Tagging
func (c *S3GenericAPI) SetTagging(ctx context.Context, bucket, prefix string, tagStr map[string]string) error {
	tags := make([]s3types.Tag, 0, len(tagStr))
	for k, v := range tagStr {
		tags = append(tags, s3types.Tag{
			Key:   aws.String(k),
			Value: aws.String(v),
		})
	}
	var err error
	if prefix == "" {
		_, err = c.S3.PutBucketTagging(ctx, &s3.PutBucketTaggingInput{
			Bucket:  aws.String(bucket),
			Tagging: &s3types.Tagging{TagSet: tags},
		})
	} else {
		_, err = c.S3.PutObjectTagging(ctx, &s3.PutObjectTaggingInput{
			Bucket:  aws.String(bucket),
			Key:     aws.String(prefix),
			Tagging: &s3types.Tagging{TagSet: tags},
		})
	}
	return err
}

func (c *S3GenericAPI) GetTagging(ctx context.Context, bucket, prefix string) (map[string]string, error) {
	var tags []s3types.Tag
	if prefix == "" {
		out, err := c.S3.GetBucketTagging(ctx, &s3.GetBucketTaggingInput{
			Bucket: aws.String(bucket)})
		if err != nil {
			return nil, err
		}
		tags = out.TagSet
	} else {
		out, err := c.S3.GetObjectTagging(ctx, &s3.GetObjectTaggingInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(prefix),
		})
		if err != nil {
			return nil, err
		}
		tags = out.TagSet
	}
	if len(tags) == 0 {
		return nil, errors.New("NoTags")
	}

	var m map[string]string
	for _, t := range tags {
		m = map[string]string{
			aws.ToString(t.Key): aws.ToString(t.Value),
		}
	}
	return m, nil
}

func (c *S3GenericAPI) DelTagging(ctx context.Context, bucket, prefix string) error {
	if prefix == "" {
		_, err := c.S3.DeleteBucketTagging(ctx, &s3.DeleteBucketTaggingInput{
			Bucket: aws.String(bucket),
		})
		return err
	}

	_, err := c.S3.DeleteObjectTagging(ctx, &s3.DeleteObjectTaggingInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(prefix),
	})
	return err
}

// ============ S3GenericAPI 实现 ============

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
		input.StorageClass = s3types.StorageClass(o.StorageClass)
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
	objects := make([]s3types.ObjectIdentifier, 0, len(keys))
	for _, k := range keys {
		objects = append(objects, s3types.ObjectIdentifier{Key: aws.String(k)})
	}

	out, err := c.S3.DeleteObjects(ctx, &s3.DeleteObjectsInput{
		Bucket: aws.String(bucket),
		Delete: &s3types.Delete{
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
	completed := make([]s3types.CompletedPart, 0, len(parts))
	for _, p := range parts {
		completed = append(completed, s3types.CompletedPart{
			PartNumber: aws.Int32(p.PartNumber),
			ETag:       aws.String(p.ETag),
		})
	}

	_, err := c.S3.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
		MultipartUpload: &s3types.CompletedMultipartUpload{
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
