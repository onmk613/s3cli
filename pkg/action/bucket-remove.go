package action

import (
	"fmt"
	myprint "s3cli/pkg/fmtutil"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func (c *S3Client) RemoveBuckets(bucket string, force bool) error {
	if force {
		myprint.PrintfYellow("!!! WARNING: --force will permanently delete all objects/versions in %v !!!", c.S3Path(bucket, ""))
		if err := c.deleteAllObjects(bucket); err != nil {
			return fmt.Errorf("force-delete objects in %s: %v", bucket, err)
		}
	}

	_, err := c.S3.DeleteBucket(c.Ctx, &s3.DeleteBucketInput{Bucket: aws.String(bucket)})
	if err != nil {
		return fmt.Errorf("delete bucket %s: %s", bucket, FormatAPIError(err))
	}
	myprint.PrintfGreen("Bucket %s deleted\n", c.S3Path(bucket, ""))
	return nil
}

func (c *S3Client) deleteAllObjects(bucket string) error {
	paginator := s3.NewListObjectVersionsPaginator(c.S3, &s3.ListObjectVersionsInput{Bucket: aws.String(bucket)})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(c.Ctx)
		if err != nil {
			return fmt.Errorf("list versions: %s", FormatAPIError(err))
		}
		objects := make([]s3types.ObjectIdentifier, 0, len(page.Versions)+len(page.DeleteMarkers))
		for _, v := range page.Versions {
			objects = append(objects, s3types.ObjectIdentifier{Key: v.Key, VersionId: v.VersionId})
		}
		for _, m := range page.DeleteMarkers {
			objects = append(objects, s3types.ObjectIdentifier{Key: m.Key, VersionId: m.VersionId})
		}
		if len(objects) == 0 {
			continue
		}
		if _, err := c.S3.DeleteObjects(c.Ctx, &s3.DeleteObjectsInput{Bucket: aws.String(bucket),
			Delete: &s3types.Delete{Objects: objects, Quiet: aws.Bool(true)},
		}); err != nil {
			return fmt.Errorf("delete objects: %s", FormatAPIError(err))
		}
	}
	return nil
}
