package action

import (
	"fmt"

	myprint "s3cli/pkg/fmtutil"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Du 显示磁盘占用, 只支持bucket及以下级别
func (c *S3Client) DuObject(bucket, prefix string) error {
	paginator := s3.NewListObjectsV2Paginator(c.S3, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket), Prefix: aws.String(prefix),
	})
	var totalSize, count int64
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(c.Ctx)
		if err != nil {
			return fmt.Errorf("list objects in %s: %s", c.S3Path(bucket, prefix), FormatAPIError(err))
		}
		for _, o := range page.Contents {
			totalSize += aws.ToInt64(o.Size)
			count++
		}
	}

	myprint.PrintfBoldGreen("%s", c.S3Path(bucket, prefix))
	myprint.PrintfBoldCyan("%5d  %12d  %10s\n", count, totalSize, FormatBytes(totalSize))
	return nil
}
