// aws-multipart.go 实现 AWS 上的分片上传: CreateMultipartUpload / UploadPart /
// CompleteMultipartUpload / AbortMultipartUpload / ListMultipartUploads / ListParts.

package awss3

import (
	"bytes"
	"context"
	"strconv"

	"s3cli/pkg/s3iface"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func (a *AWS) CreateMultipartUpload(ctx context.Context, bucket, key string, opts *s3iface.PutObjectOptions) (*s3iface.CreateMultipartUploadOutput, error) {
	input := &s3.CreateMultipartUploadInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}
	if opts != nil {
		applyPutOptsToCreate(input, opts)
	}
	out, err := a.client.CreateMultipartUpload(ctx, input)
	if err != nil {
		return nil, sdkErr(err)
	}
	return &s3iface.CreateMultipartUploadOutput{
		UploadID:             aws.ToString(out.UploadId),
		ServerSideEncryption: string(out.ServerSideEncryption),
		SSEKMSKeyID:          aws.ToString(out.SSEKMSKeyId),
	}, nil
}

func applyPutOptsToCreate(input *s3.CreateMultipartUploadInput, opts *s3iface.PutObjectOptions) {
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
	if opts.ObjectLockLegalHold != "" {
		input.ObjectLockLegalHoldStatus = types.ObjectLockLegalHoldStatus(opts.ObjectLockLegalHold)
	}
}

func (a *AWS) UploadPart(ctx context.Context, bucket, key, uploadID string, partNumber int, body []byte) (*s3iface.UploadPartOutput, error) {
	out, err := a.client.UploadPart(ctx, &s3.UploadPartInput{
		Bucket:     aws.String(bucket),
		Key:        aws.String(key),
		UploadId:   aws.String(uploadID),
		PartNumber: aws.Int32(int32(partNumber)),
		Body:       bytes.NewReader(body),
	})
	if err != nil {
		return nil, sdkErr(err)
	}
	return &s3iface.UploadPartOutput{
		ETag:                 aws.ToString(out.ETag),
		SSEKMSKeyID:          aws.ToString(out.SSEKMSKeyId),
		ServerSideEncryption: string(out.ServerSideEncryption),
	}, nil
}

func (a *AWS) CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string, parts []s3iface.CompletedPart) (*s3iface.CompleteMultipartUploadOutput, error) {
	sdkParts := make([]types.CompletedPart, 0, len(parts))
	for _, p := range parts {
		sdkParts = append(sdkParts, types.CompletedPart{
			ETag:       aws.String(p.ETag),
			PartNumber: aws.Int32(int32(p.PartNumber)),
		})
	}
	out, err := a.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:          aws.String(bucket),
		Key:             aws.String(key),
		UploadId:        aws.String(uploadID),
		MultipartUpload: &types.CompletedMultipartUpload{Parts: sdkParts},
	})
	if err != nil {
		return nil, sdkErr(err)
	}
	return &s3iface.CompleteMultipartUploadOutput{
		Location:             aws.ToString(out.Location),
		Bucket:               aws.ToString(out.Bucket),
		Key:                  aws.ToString(out.Key),
		ETag:                 aws.ToString(out.ETag),
		VersionID:            aws.ToString(out.VersionId),
		ServerSideEncryption: string(out.ServerSideEncryption),
		SSEKMSKeyID:          aws.ToString(out.SSEKMSKeyId),
	}, nil
}

func (a *AWS) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	_, err := a.client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
	})
	return sdkErr(err)
}

func (a *AWS) ListMultipartUploads(ctx context.Context, bucket string, opts *s3iface.ListMultipartUploadsOptions) (*s3iface.ListMultipartUploadsOutput, error) {
	if opts == nil {
		opts = &s3iface.ListMultipartUploadsOptions{}
	}
	input := &s3.ListMultipartUploadsInput{Bucket: aws.String(bucket)}
	if opts.Prefix != "" {
		input.Prefix = aws.String(opts.Prefix)
	}
	if opts.Delimiter != "" {
		input.Delimiter = aws.String(opts.Delimiter)
	}
	if opts.KeyMarker != "" {
		input.KeyMarker = aws.String(opts.KeyMarker)
	}
	if opts.UploadIDMarker != "" {
		input.UploadIdMarker = aws.String(opts.UploadIDMarker)
	}
	if opts.MaxUploads > 0 {
		input.MaxUploads = aws.Int32(int32(opts.MaxUploads))
	}

	out, err := a.client.ListMultipartUploads(ctx, input)
	if err != nil {
		return nil, sdkErr(err)
	}
	result := &s3iface.ListMultipartUploadsOutput{
		Bucket:             aws.ToString(out.Bucket),
		KeyMarker:          aws.ToString(out.KeyMarker),
		UploadIDMarker:     aws.ToString(out.UploadIdMarker),
		NextKeyMarker:      aws.ToString(out.NextKeyMarker),
		NextUploadIDMarker: aws.ToString(out.NextUploadIdMarker),
		MaxUploads:         int(aws.ToInt32(out.MaxUploads)),
		IsTruncated:        aws.ToBool(out.IsTruncated),
	}
	for _, u := range out.Uploads {
		result.Uploads = append(result.Uploads, toUploadInfo(u))
	}
	return result, nil
}

func (a *AWS) ListParts(ctx context.Context, bucket, key, uploadID string, partNumberMarker, maxParts int) (*s3iface.ListPartsOutput, error) {
	input := &s3.ListPartsInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
	}
	if partNumberMarker > 0 {
		input.PartNumberMarker = aws.String(strconv.Itoa(partNumberMarker))
	}
	if maxParts > 0 {
		input.MaxParts = aws.Int32(int32(maxParts))
	}
	out, err := a.client.ListParts(ctx, input)
	if err != nil {
		return nil, sdkErr(err)
	}
	result := &s3iface.ListPartsOutput{
		Bucket:               aws.ToString(out.Bucket),
		Key:                  aws.ToString(out.Key),
		UploadID:             aws.ToString(out.UploadId),
		PartNumberMarker:     parsePartMarker(aws.ToString(out.PartNumberMarker)),
		NextPartNumberMarker: parsePartMarker(aws.ToString(out.NextPartNumberMarker)),
		MaxParts:             int(aws.ToInt32(out.MaxParts)),
		IsTruncated:          aws.ToBool(out.IsTruncated),
	}
	for _, p := range out.Parts {
		result.Parts = append(result.Parts, toPartInfo(p))
	}
	return result, nil
}

func parsePartMarker(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0
		}
		n = n*10 + int(s[i]-'0')
	}
	return n
}
