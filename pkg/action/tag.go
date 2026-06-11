package action

import (
	"fmt"

	myprint "s3cli/pkg/fmtutil"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func (c *S3Client) SetTag(bucket, prefix string, tagStr map[string]string) error {
	tags := parseTagPairs(tagStr)
	if prefix == "" {
		_, err := c.S3.PutBucketTagging(c.Ctx, &s3.PutBucketTaggingInput{
			Bucket:  aws.String(bucket),
			Tagging: &s3types.Tagging{TagSet: tags},
		})
		if err != nil {
			return fmt.Errorf("set bucket tag: %s", FormatAPIError(err))
		}
		myprint.PrintfBoldGreen("Tag set for %s (%d tags)\n", c.S3Path(bucket, prefix), len(tags))
		return nil
	}

	_, err := c.S3.PutObjectTagging(c.Ctx, &s3.PutObjectTaggingInput{
		Bucket: aws.String(bucket), Key: aws.String(prefix),
		Tagging: &s3types.Tagging{TagSet: tags},
	})
	if err != nil {
		return fmt.Errorf("set object tag: %s", FormatAPIError(err))
	}

	myprint.PrintfBoldGreen("tag set for %s (%d tags)\n", c.S3Path(bucket, prefix), len(tags))
	return nil
}

func (c *S3Client) GetTag(bucket, prefix string) error {
	var tags []s3types.Tag
	if prefix == "" {
		out, err := c.S3.GetBucketTagging(c.Ctx, &s3.GetBucketTaggingInput{Bucket: aws.String(bucket)})
		if err != nil {
			return fmt.Errorf("get bucket tag: %s", FormatAPIError(err))
		}
		tags = out.TagSet
	} else {
		out, err := c.S3.GetObjectTagging(c.Ctx, &s3.GetObjectTaggingInput{
			Bucket: aws.String(bucket), Key: aws.String(prefix),
		})
		if err != nil {
			return fmt.Errorf("get object tag: %s", FormatAPIError(err))
		}
		tags = out.TagSet
	}
	if len(tags) == 0 {
		myprint.PrintfCyan("# %s: no tags\n", c.S3Path(bucket, prefix))
		return nil
	}

	myprint.PrintfBoldBlue("# %s tags:\n", c.S3Path(bucket, prefix))
	for _, t := range tags {
		myprint.PrintfGreen("  %s = %s\n", aws.ToString(t.Key), aws.ToString(t.Value))
	}
	return nil
}

func (c *S3Client) DelTag(bucket, prefix string) error {
	if prefix == "" {
		if _, err := c.S3.DeleteBucketTagging(c.Ctx,
			&s3.DeleteBucketTaggingInput{Bucket: aws.String(bucket)}); err != nil {
			return fmt.Errorf("delete bucket tag: %s", FormatAPIError(err))
		}
	} else {
		if _, err := c.S3.DeleteObjectTagging(c.Ctx,
			&s3.DeleteObjectTaggingInput{Bucket: aws.String(bucket), Key: aws.String(prefix)}); err != nil {
			return fmt.Errorf("delete object tag: %s", FormatAPIError(err))
		}
	}

	myprint.PrintfBoldGreen("Tags deleted for %s\n", c.S3Path(bucket, prefix))
	return nil
}

func parseTagPairs(m map[string]string) []s3types.Tag {
	tags := make([]s3types.Tag, 0, len(m))
	for k, v := range m {
		tags = append(tags, s3types.Tag{
			Key:   aws.String(k),
			Value: aws.String(v),
		})
	}
	return tags
}
