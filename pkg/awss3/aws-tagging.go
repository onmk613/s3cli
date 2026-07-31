//go:build aws

// aws-tagging.go 实现 AWS 上的标签管理: 桶标签与对象标签的 Set/Get/Delete.

package awss3

import (
	"context"

	"s3cli/pkg/s3iface"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func toSDKTags(tags []s3iface.Tagging) []types.Tag {
	result := make([]types.Tag, 0, len(tags))
	for _, t := range tags {
		result = append(result, types.Tag{
			Key:   aws.String(t.Key),
			Value: aws.String(t.Value),
		})
	}
	return result
}

func fromSDKTags(tags []types.Tag) []s3iface.Tagging {
	result := make([]s3iface.Tagging, 0, len(tags))
	for _, t := range tags {
		result = append(result, s3iface.Tagging{
			Key:   aws.ToString(t.Key),
			Value: aws.ToString(t.Value),
		})
	}
	return result
}

// ---- 对象标签 ----

func (a *AWS) SetObjectTagging(ctx context.Context, bucket, key string, tags []s3iface.Tagging, versionID string) error {
	input := &s3.PutObjectTaggingInput{
		Bucket:  aws.String(bucket),
		Key:     aws.String(key),
		Tagging: &types.Tagging{TagSet: toSDKTags(tags)},
	}
	if versionID != "" {
		input.VersionId = aws.String(versionID)
	}
	_, err := a.client.PutObjectTagging(ctx, input)
	return sdkErr(err)
}

func (a *AWS) GetObjectTagging(ctx context.Context, bucket, key, versionID string) ([]s3iface.Tagging, error) {
	input := &s3.GetObjectTaggingInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}
	if versionID != "" {
		input.VersionId = aws.String(versionID)
	}
	out, err := a.client.GetObjectTagging(ctx, input)
	if err != nil {
		return nil, sdkErr(err)
	}
	return fromSDKTags(out.TagSet), nil
}

func (a *AWS) DeleteObjectTagging(ctx context.Context, bucket, key, versionID string) error {
	input := &s3.DeleteObjectTaggingInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}
	if versionID != "" {
		input.VersionId = aws.String(versionID)
	}
	_, err := a.client.DeleteObjectTagging(ctx, input)
	return sdkErr(err)
}

// ---- 桶标签 ----

func (a *AWS) SetBucketTagging(ctx context.Context, bucket string, tags []s3iface.Tagging) error {
	_, err := a.client.PutBucketTagging(ctx, &s3.PutBucketTaggingInput{
		Bucket:  aws.String(bucket),
		Tagging: &types.Tagging{TagSet: toSDKTags(tags)},
	})
	return sdkErr(err)
}

func (a *AWS) GetBucketTagging(ctx context.Context, bucket string) ([]s3iface.Tagging, error) {
	out, err := a.client.GetBucketTagging(ctx, &s3.GetBucketTaggingInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		return nil, sdkErr(err)
	}
	return fromSDKTags(out.TagSet), nil
}

func (a *AWS) DeleteBucketTagging(ctx context.Context, bucket string) error {
	_, err := a.client.DeleteBucketTagging(ctx, &s3.DeleteBucketTaggingInput{
		Bucket: aws.String(bucket),
	})
	return sdkErr(err)
}
