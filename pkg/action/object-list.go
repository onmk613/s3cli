package action

import (
	"fmt"

	myprint "s3cli/pkg/fmtutil"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func (c *S3Client) ListObjects(bucket, prefix string, listAll bool) error {
	if bucket == "" {
		result, err := c.S3.ListBuckets(c.Ctx, nil)
		if err != nil {
			return fmt.Errorf("list buckets: %s", FormatAPIError(err))
		}
		for _, bucket := range result.Buckets {
			myprint.PrintfDim("[%s]   ", bucket.CreationDate.Format("2006-01-02 15:04"))
			myprint.PrintfGreen("%s\n", c.S3Path(aws.ToString(bucket.Name), ""))
		}
		return nil
	}
	return c.listObjectsV2(bucket, prefix, listAll)
}

func (c *S3Client) listObjectsV2(bucket, prefix string, listAll bool) error {
	var paginator *s3.ListObjectsV2Paginator

	if listAll {
		paginator = s3.NewListObjectsV2Paginator(c.S3, &s3.ListObjectsV2Input{
			Bucket: aws.String(bucket), Prefix: aws.String(prefix),
		})
	} else {
		paginator = s3.NewListObjectsV2Paginator(c.S3, &s3.ListObjectsV2Input{
			Bucket: aws.String(bucket), Prefix: aws.String(prefix),
			Delimiter: aws.String("/"),
		})
	}

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(c.Ctx)
		if err != nil {
			return fmt.Errorf("list objects: %s", FormatAPIError(err))
		}
		for _, p := range page.CommonPrefixes {
			myprint.PrintfBlue("%-22s %12s   DIR   %s\n", "", "-", c.S3Path(bucket, aws.ToString(p.Prefix)))
		}
		for _, item := range page.Contents {
			myprint.PrintfDim("[%s]  ", item.LastModified.Format("2006-01-02 15:04:05"))
			myprint.Printf("%12d   ", aws.ToInt64(item.Size))
			myprint.PrintfGreen("FILE  %s\n", c.S3Path(bucket, aws.ToString(item.Key)))
		}
	}
	return nil
}
