//go:build aws

// aws-object.go 实现 AWS 上的对象操作: HeadObject / GetObject / PutObject / PutObjectStream /
// CopyObject / DeleteObject / DeleteObjects.

package awss3

import (
	"bytes"
	"context"
	"io"
	"time"

	"s3cli/pkg/s3iface"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func (a *AWS) HeadObject(ctx context.Context, bucket, key, versionID string) (*s3iface.HeadObjectOutput, error) {
	input := &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}
	if versionID != "" {
		input.VersionId = aws.String(versionID)
	}
	out, err := a.client.HeadObject(ctx, input)
	if err != nil {
		return nil, sdkErr(err)
	}
	return &s3iface.HeadObjectOutput{
		ContentLength:             aws.ToInt64(out.ContentLength),
		ContentType:               aws.ToString(out.ContentType),
		ContentEncoding:           aws.ToString(out.ContentEncoding),
		ContentDisposition:        aws.ToString(out.ContentDisposition),
		ContentLanguage:           aws.ToString(out.ContentLanguage),
		CacheControl:              aws.ToString(out.CacheControl),
		Expires:                   aws.ToString(out.ExpiresString),
		ETag:                      aws.ToString(out.ETag),
		LastModified:              aws.ToTime(out.LastModified),
		StorageClass:              string(out.StorageClass),
		VersionID:                 aws.ToString(out.VersionId),
		DeleteMarker:              aws.ToBool(out.DeleteMarker),
		ServerSideEncryption:      string(out.ServerSideEncryption),
		SSEKMSKeyID:               aws.ToString(out.SSEKMSKeyId),
		SSECustomerAlgorithm:      aws.ToString(out.SSECustomerAlgorithm),
		SSECustomerKeyMD5:         aws.ToString(out.SSECustomerKeyMD5),
		PartsCount:                aws.ToInt32(out.PartsCount),
		ReplicationStatus:         string(out.ReplicationStatus),
		ObjectLockMode:            string(out.ObjectLockMode),
		ObjectLockRetainUntilDate: aws.ToTime(out.ObjectLockRetainUntilDate),
		ObjectLockLegalHold:       string(out.ObjectLockLegalHoldStatus),
		Metadata:                  out.Metadata,
	}, nil
}

func (a *AWS) GetObject(ctx context.Context, bucket, key string, opts *s3iface.GetObjectOptions) (*s3iface.GetObjectOutput, error) {
	input := &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}
	if opts != nil {
		if opts.VersionID != "" {
			input.VersionId = aws.String(opts.VersionID)
		}
		if opts.Range != "" {
			input.Range = aws.String(opts.Range)
		}
		if opts.IfMatch != "" {
			input.IfMatch = aws.String(opts.IfMatch)
		}
		if opts.IfNoneMatch != "" {
			input.IfNoneMatch = aws.String(opts.IfNoneMatch)
		}
		if opts.IfModifiedSince != nil {
			input.IfModifiedSince = opts.IfModifiedSince
		}
		if opts.IfUnmodifiedSince != nil {
			input.IfUnmodifiedSince = opts.IfUnmodifiedSince
		}
		if opts.ResponseContentType != "" {
			input.ResponseContentType = aws.String(opts.ResponseContentType)
		}
		if opts.ResponseContentEncoding != "" {
			input.ResponseContentEncoding = aws.String(opts.ResponseContentEncoding)
		}
		if opts.ResponseContentDisposition != "" {
			input.ResponseContentDisposition = aws.String(opts.ResponseContentDisposition)
		}
		if opts.ResponseCacheControl != "" {
			input.ResponseCacheControl = aws.String(opts.ResponseCacheControl)
		}
		if opts.ResponseExpires != "" {
			if t, err := time.Parse(time.RFC1123, opts.ResponseExpires); err == nil {
				input.ResponseExpires = &t
			}
		}
	}
	out, err := a.client.GetObject(ctx, input)
	if err != nil {
		return nil, sdkErr(err)
	}
	return &s3iface.GetObjectOutput{
		Body:                      out.Body,
		ContentLength:             aws.ToInt64(out.ContentLength),
		ContentType:               aws.ToString(out.ContentType),
		ContentEncoding:           aws.ToString(out.ContentEncoding),
		ContentDisposition:        aws.ToString(out.ContentDisposition),
		ContentLanguage:           aws.ToString(out.ContentLanguage),
		CacheControl:              aws.ToString(out.CacheControl),
		Expires:                   aws.ToString(out.ExpiresString),
		ETag:                      aws.ToString(out.ETag),
		LastModified:              aws.ToTime(out.LastModified),
		StorageClass:              string(out.StorageClass),
		VersionID:                 aws.ToString(out.VersionId),
		DeleteMarker:              aws.ToBool(out.DeleteMarker),
		ServerSideEncryption:      string(out.ServerSideEncryption),
		SSEKMSKeyID:               aws.ToString(out.SSEKMSKeyId),
		SSECustomerAlgorithm:      aws.ToString(out.SSECustomerAlgorithm),
		SSECustomerKeyMD5:         aws.ToString(out.SSECustomerKeyMD5),
		PartsCount:                aws.ToInt32(out.PartsCount),
		ReplicationStatus:         string(out.ReplicationStatus),
		ObjectLockMode:            string(out.ObjectLockMode),
		ObjectLockRetainUntilDate: aws.ToTime(out.ObjectLockRetainUntilDate),
		ObjectLockLegalHold:       string(out.ObjectLockLegalHoldStatus),
		AcceptRanges:              aws.ToString(out.AcceptRanges),
		Metadata:                  out.Metadata,
	}, nil
}

func putInputFromOpts(bucket, key string, opts *s3iface.PutObjectOptions) *s3.PutObjectInput {
	input := &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)}
	if opts == nil {
		return input
	}
	if opts.ContentType != "" {
		input.ContentType = aws.String(opts.ContentType)
	}
	if opts.ContentEncoding != "" {
		input.ContentEncoding = aws.String(opts.ContentEncoding)
	}
	if opts.ContentDisposition != "" {
		input.ContentDisposition = aws.String(opts.ContentDisposition)
	}
	if opts.ContentLanguage != "" {
		input.ContentLanguage = aws.String(opts.ContentLanguage)
	}
	if opts.CacheControl != "" {
		input.CacheControl = aws.String(opts.CacheControl)
	}
	if opts.StorageClass != "" {
		input.StorageClass = types.StorageClass(opts.StorageClass)
	}
	if len(opts.Metadata) > 0 {
		input.Metadata = opts.Metadata
	}
	if opts.ServerSideEncryption != "" {
		input.ServerSideEncryption = types.ServerSideEncryption(opts.ServerSideEncryption)
	}
	if opts.SSEKMSKeyID != "" {
		input.SSEKMSKeyId = aws.String(opts.SSEKMSKeyID)
	}
	if opts.ObjectLockMode != "" {
		input.ObjectLockMode = types.ObjectLockMode(opts.ObjectLockMode)
	}
	if opts.ObjectLockRetainUntilDate != "" {
		if t, err := time.Parse(time.RFC3339, opts.ObjectLockRetainUntilDate); err == nil {
			input.ObjectLockRetainUntilDate = &t
		}
	}
	if opts.ObjectLockLegalHold != "" {
		input.ObjectLockLegalHoldStatus = types.ObjectLockLegalHoldStatus(opts.ObjectLockLegalHold)
	}
	return input
}

func (a *AWS) PutObject(ctx context.Context, bucket, key string, body []byte, opts *s3iface.PutObjectOptions) (*s3iface.PutObjectOutput, error) {
	input := putInputFromOpts(bucket, key, opts)
	input.Body = bytes.NewReader(body)
	out, err := a.client.PutObject(ctx, input)
	if err != nil {
		return nil, sdkErr(err)
	}
	return &s3iface.PutObjectOutput{
		ETag:                 aws.ToString(out.ETag),
		VersionID:            aws.ToString(out.VersionId),
		ServerSideEncryption: string(out.ServerSideEncryption),
		SSEKMSKeyID:          aws.ToString(out.SSEKMSKeyId),
	}, nil
}

func (a *AWS) PutObjectStream(ctx context.Context, bucket, key string, body io.ReadSeeker, contentLength int64, opts *s3iface.PutObjectOptions) (*s3iface.PutObjectOutput, error) {
	input := putInputFromOpts(bucket, key, opts)
	input.Body = body
	if contentLength > 0 {
		input.ContentLength = aws.Int64(contentLength)
	}
	out, err := a.client.PutObject(ctx, input)
	if err != nil {
		return nil, sdkErr(err)
	}
	return &s3iface.PutObjectOutput{
		ETag:                 aws.ToString(out.ETag),
		VersionID:            aws.ToString(out.VersionId),
		ServerSideEncryption: string(out.ServerSideEncryption),
		SSEKMSKeyID:          aws.ToString(out.SSEKMSKeyId),
	}, nil
}

func (a *AWS) CopyObject(ctx context.Context, srcBucket, srcKey, destBucket, destKey string, opts *s3iface.CopyObjectOptions) (*s3iface.CopyObjectOutput, error) {
	copySource := "/" + srcBucket + "/" + srcKey
	if opts != nil && opts.SourceVersionID != "" {
		copySource += "?versionId=" + opts.SourceVersionID
	}
	input := &s3.CopyObjectInput{
		Bucket:     aws.String(destBucket),
		Key:        aws.String(destKey),
		CopySource: aws.String(copySource),
	}
	if opts != nil {
		if opts.MetadataDirective != "" {
			input.MetadataDirective = types.MetadataDirective(opts.MetadataDirective)
		}
		if opts.TaggingDirective != "" {
			input.TaggingDirective = types.TaggingDirective(opts.TaggingDirective)
		}
		if opts.Tagging != "" {
			input.Tagging = aws.String(opts.Tagging)
		}
		if opts.StorageClass != "" {
			input.StorageClass = types.StorageClass(opts.StorageClass)
		}
		if opts.ContentType != "" {
			input.ContentType = aws.String(opts.ContentType)
		}
		if opts.ServerSideEncryption != "" {
			input.ServerSideEncryption = types.ServerSideEncryption(opts.ServerSideEncryption)
		}
		if opts.SSEKMSKeyID != "" {
			input.SSEKMSKeyId = aws.String(opts.SSEKMSKeyID)
		}
		if opts.IfMatch != "" {
			input.CopySourceIfMatch = aws.String(opts.IfMatch)
		}
		if opts.IfNoneMatch != "" {
			input.CopySourceIfNoneMatch = aws.String(opts.IfNoneMatch)
		}
		if opts.IfModifiedSince != "" {
			if t, err := time.Parse(time.RFC1123, opts.IfModifiedSince); err == nil {
				input.CopySourceIfModifiedSince = &t
			}
		}
		if opts.IfUnmodifiedSince != "" {
			if t, err := time.Parse(time.RFC1123, opts.IfUnmodifiedSince); err == nil {
				input.CopySourceIfUnmodifiedSince = &t
			}
		}
		if len(opts.Metadata) > 0 {
			input.Metadata = opts.Metadata
		}
	}
	out, err := a.client.CopyObject(ctx, input)
	if err != nil {
		return nil, sdkErr(err)
	}
	result := &s3iface.CopyObjectOutput{
		VersionID:            aws.ToString(out.VersionId),
		ServerSideEncryption: string(out.ServerSideEncryption),
		SSEKMSKeyID:          aws.ToString(out.SSEKMSKeyId),
	}
	if out.CopyObjectResult != nil {
		result.ETag = aws.ToString(out.CopyObjectResult.ETag)
		result.LastModified = aws.ToTime(out.CopyObjectResult.LastModified).Format(time.RFC3339)
	}
	return result, nil
}

func (a *AWS) DeleteObject(ctx context.Context, bucket, key, versionID string) (*s3iface.DeleteObjectOutput, error) {
	input := &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}
	if versionID != "" {
		input.VersionId = aws.String(versionID)
	}
	out, err := a.client.DeleteObject(ctx, input)
	if err != nil {
		return nil, sdkErr(err)
	}
	return &s3iface.DeleteObjectOutput{
		VersionID:    aws.ToString(out.VersionId),
		DeleteMarker: aws.ToBool(out.DeleteMarker),
	}, nil
}

func (a *AWS) DeleteObjects(ctx context.Context, bucket string, objects []s3iface.ObjectIdentifier, quiet bool) (*s3iface.DeleteObjectsOutput, error) {
	ids := make([]types.ObjectIdentifier, 0, len(objects))
	for _, o := range objects {
		id := types.ObjectIdentifier{Key: aws.String(o.Key)}
		if o.VersionID != "" {
			id.VersionId = aws.String(o.VersionID)
		}
		ids = append(ids, id)
	}
	out, err := a.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
		Bucket: aws.String(bucket),
		Delete: &types.Delete{
			Objects: ids,
			Quiet:   aws.Bool(quiet),
		},
	})
	if err != nil {
		return nil, sdkErr(err)
	}
	result := &s3iface.DeleteObjectsOutput{}
	for _, d := range out.Deleted {
		result.Deleted = append(result.Deleted, s3iface.DeletedObject{
			Key:          aws.ToString(d.Key),
			VersionID:    aws.ToString(d.VersionId),
			DeleteMarker: aws.ToBool(d.DeleteMarker),
		})
	}
	for _, e := range out.Errors {
		result.Errors = append(result.Errors, s3iface.DeleteObjectError{
			Key:     aws.ToString(e.Key),
			Code:    aws.ToString(e.Code),
			Message: aws.ToString(e.Message),
		})
	}
	return result, nil
}
