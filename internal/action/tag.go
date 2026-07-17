package action

import (
	"fmt"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/s3api"
)

func (c *S3Client) SetTag(bucket, prefix string, tagStr map[string]string) error {
	tags := parseTagPairs(tagStr)
	if prefix == "" {
		if err := c.S3.SetBucketTagging(c.Ctx, bucket, tags); err != nil {
			return fmt.Errorf("set bucket tag: %s", FormatAPIError(err))
		}
		myprint.PrintfBoldGreen("Tag set for %s (%d tags)\n", c.S3Path(bucket, prefix), len(tags))
		return nil
	}

	if err := c.S3.SetObjectTagging(c.Ctx, bucket, prefix, tags, ""); err != nil {
		return fmt.Errorf("set object tag: %s", FormatAPIError(err))
	}

	myprint.PrintfBoldGreen("Tag set for %s (%d tags)\n", c.S3Path(bucket, prefix), len(tags))
	return nil
}

func (c *S3Client) GetTag(bucket, prefix string) error {
	var tags []s3api.Tagging
	if prefix == "" {
		result, err := c.S3.GetBucketTagging(c.Ctx, bucket)
		if err != nil {
			return fmt.Errorf("get bucket tag: %s", FormatAPIError(err))
		}
		tags = result
	} else {
		result, err := c.S3.GetObjectTagging(c.Ctx, bucket, prefix, "")
		if err != nil {
			return fmt.Errorf("get object tag: %s", FormatAPIError(err))
		}
		tags = result
	}
	if len(tags) == 0 {
		myprint.PrintfCyan("# %s: no tags\n", c.S3Path(bucket, prefix))
		return nil
	}

	myprint.PrintfBoldBlue("# %s tags:\n", c.S3Path(bucket, prefix))
	for _, t := range tags {
		myprint.PrintfGreen("  %s = %s\n", t.Key, t.Value)
	}
	return nil
}

func (c *S3Client) DelTag(bucket, prefix string) error {
	if prefix == "" {
		if err := c.S3.DeleteBucketTagging(c.Ctx, bucket); err != nil {
			return fmt.Errorf("delete bucket tag: %s", FormatAPIError(err))
		}
	} else {
		if err := c.S3.DeleteObjectTagging(c.Ctx, bucket, prefix, ""); err != nil {
			return fmt.Errorf("delete object tag: %s", FormatAPIError(err))
		}
	}

	myprint.PrintfBoldGreen("Tags deleted for %s\n", c.S3Path(bucket, prefix))
	return nil
}

func parseTagPairs(m map[string]string) []s3api.Tagging {
	tags := make([]s3api.Tagging, 0, len(m))
	for k, v := range m {
		tags = append(tags, s3api.Tagging{
			Key:   k,
			Value: v,
		})
	}
	return tags
}
