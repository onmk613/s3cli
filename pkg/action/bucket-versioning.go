package action

import (
	"fmt"

	myprint "s3cli/pkg/fmtutil"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func (c *S3Client) SetVersioning(bucket string, status bool) error {
	var st s3types.BucketVersioningStatus
	if status {
		st = s3types.BucketVersioningStatusEnabled
	} else {
		st = s3types.BucketVersioningStatusSuspended
	}

	_, err := c.S3.PutBucketVersioning(c.Ctx,
		&s3.PutBucketVersioningInput{
			Bucket: aws.String(bucket),
			VersioningConfiguration: &s3types.VersioningConfiguration{
				Status: s3types.BucketVersioningStatus(st),
			},
		})
	if err != nil {
		return fmt.Errorf("set versioning %s: %s", bucket, FormatAPIError(err))
	}

	myprint.PrintfBoldGreen("Versioning %s for %s %s\n", st, c.Alias, bucket)
	return nil
}

func (c *S3Client) GetVersioning(bucket string) error {
	out, err := c.S3.GetBucketVersioning(c.Ctx,
		&s3.GetBucketVersioningInput{Bucket: aws.String(bucket)})
	if err != nil {
		return fmt.Errorf("get versioning %s: %s", bucket, FormatAPIError(err))
	}
	status := string(out.Status)
	if status == "" {
		status = "(disabled)"
	}

	myprint.PrintfBoldGreen("Bucket %s versioning for%s: %s\n", c.Alias, bucket, status)
	return nil
}
