//go:build aws

// aws-bucket.go 实现 AWS 上的桶基础操作: ListBuckets / CreateBucket / DeleteBucket / GetBucketLocation.

package awss3

import (
	"context"

	"s3cli/pkg/s3iface"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func (a *AWS) ListBuckets(ctx context.Context) ([]s3iface.BucketInfo, error) {
	out, err := a.client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return nil, sdkErr(err)
	}
	result := make([]s3iface.BucketInfo, 0, len(out.Buckets))
	for _, b := range out.Buckets {
		result = append(result, toBucketInfo(b))
	}
	return result, nil
}

func (a *AWS) CreateBucket(ctx context.Context, bucketName string, opts *s3iface.MakeBucketOptions) error {
	input := &s3.CreateBucketInput{Bucket: aws.String(bucketName)}
	if opts != nil {
		if opts.Region != "" && opts.Region != "us-east-1" {
			input.CreateBucketConfiguration = &types.CreateBucketConfiguration{
				LocationConstraint: types.BucketLocationConstraint(opts.Region),
			}
		}
	}
	_, err := a.client.CreateBucket(ctx, input)
	return sdkErr(err)
}

func (a *AWS) DeleteBucket(ctx context.Context, bucketName string) error {
	_, err := a.client.DeleteBucket(ctx, &s3.DeleteBucketInput{
		Bucket: aws.String(bucketName),
	})
	return sdkErr(err)
}

func (a *AWS) GetBucketLocation(ctx context.Context, bucket string) (string, error) {
	out, err := a.client.GetBucketLocation(ctx, &s3.GetBucketLocationInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		return "", sdkErr(err)
	}
	return string(out.LocationConstraint), nil
}
