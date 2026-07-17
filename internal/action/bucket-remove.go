package action

import (
	"fmt"
	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/s3api"
)

func (c *S3Client) RemoveBuckets(bucket string, force bool) error {
	if force {
		myprint.PrintfBoldYellow("!!! WARNING: --force will permanently delete all objects/versions in %v %s!!!", c.Alias, bucket)
		if err := c.deleteAllObjects(bucket); err != nil {
			return fmt.Errorf("force-delete objects in %s: %v", bucket, err)
		}
	}

	if err := c.S3.DeleteBucket(c.Ctx, bucket); err != nil {
		return fmt.Errorf("delete bucket %s: %s", bucket, FormatAPIError(err))
	}

	myprint.PrintfBoldGreen("Bucket %s deleted for %s\n", c.Alias, bucket)
	return nil
}

func (c *S3Client) deleteAllObjects(bucket string) error {
	paginator := s3api.NewListObjectVersionsPaginator(c.S3, bucket, &s3api.ListObjectVersionsOptions{})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(c.Ctx)
		if err != nil {
			return fmt.Errorf("list versions: %s", FormatAPIError(err))
		}
		objects := make([]s3api.ObjectIdentifier, 0, len(page.Versions)+len(page.DeleteMarkers))
		for _, v := range page.Versions {
			objects = append(objects, s3api.ObjectIdentifier{Key: v.Key, VersionID: v.VersionID})
		}
		for _, m := range page.DeleteMarkers {
			objects = append(objects, s3api.ObjectIdentifier{Key: m.Key, VersionID: m.VersionID})
		}
		if len(objects) == 0 {
			continue
		}
		if _, err := c.S3.DeleteObjects(c.Ctx, bucket, objects, true); err != nil {
			return fmt.Errorf("delete objects: %s", FormatAPIError(err))
		}
	}
	return nil
}
