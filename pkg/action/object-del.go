package action

import (
	"fmt"

	myprint "s3cli/pkg/fmtutil"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// Rm 删除对象 / 目录
func (c *S3Client) DeleteObjects(bucket, prefix string, recursive bool) error {
	ok, err := c.IsS3File(bucket, prefix)
	if err != nil {
		return fmt.Errorf("check s3 path: %s", FormatAPIError(err))
	}
	switch {
	case !ok && recursive:
		if err := c.deleteObjectsWithPrefix(bucket, prefix); err != nil {
			return err
		}
	case !ok && !recursive:
		return fmt.Errorf("s3://%s/%s: not a single object. Use -r/--recursive to delete a directory.", bucket, prefix)
	default:
		if err := c.deleteSingleObject(bucket, prefix); err != nil {
			return err
		}
	}
	return nil
}

func (c *S3Client) deleteSingleObject(bucket, key string) error {
	_, err := c.S3.DeleteObject(c.Ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket), Key: aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("delete s3://%s/%s: %s", bucket, key, FormatAPIError(err))
	}
	myprint.PrintfGreen("delete: %s\n", c.S3Path(bucket, key))
	return nil
}

func (c *S3Client) deleteObjectsWithPrefix(bucket, prefix string) error {
	paginator := s3.NewListObjectsV2Paginator(c.S3, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket), Prefix: aws.String(prefix),
	})

	var toDelete []types.ObjectIdentifier
	var total int
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(c.Ctx)
		if err != nil {
			return fmt.Errorf("list objects: %s", FormatAPIError(err))
		}
		for _, item := range page.Contents {
			toDelete = append(toDelete, types.ObjectIdentifier{Key: item.Key})
		}
		if len(toDelete) >= 1000 {
			if err := c.deleteBatch(bucket, toDelete); err != nil {
				return err
			}
			total += len(toDelete)
			toDelete = toDelete[:0]
		}
	}
	if len(toDelete) > 0 {
		if err := c.deleteBatch(bucket, toDelete); err != nil {
			return err
		}
		total += len(toDelete)
	}
	if prefix == "" {
		myprint.PrintfGreen("delete: %d objects from %s\n", total, c.S3Path(bucket, ""))
	} else {
		myprint.PrintfGreen("delete: %d objects from %s\n", total, c.S3Path(bucket, prefix))
	}
	return nil
}

func (c *S3Client) deleteBatch(bucket string, objects []types.ObjectIdentifier) error {
	_, err := c.S3.DeleteObjects(c.Ctx, &s3.DeleteObjectsInput{
		Bucket: aws.String(bucket),
		Delete: &types.Delete{Objects: objects, Quiet: aws.Bool(true)},
	})
	if err != nil {
		return fmt.Errorf("delete batch of %d: %s", len(objects), FormatAPIError(err))
	}
	return nil
}
