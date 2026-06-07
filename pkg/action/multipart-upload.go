package action

import (
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// MpuList 列出未完成的 multipart upload
func (c *S3Client) MpuList(bucket, prefix string) error {
	paginator := s3.NewListMultipartUploadsPaginator(c.S3, &s3.ListMultipartUploadsInput{Bucket: aws.String(bucket), Prefix: aws.String(prefix)})
	var count int
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(c.Ctx)
		if err != nil {
			return fmt.Errorf("list multipart uploads: %s", FormatAPIError(err))
		}
		for _, u := range page.Uploads {
			initiated := ""
			if u.Initiated != nil {
				initiated = u.Initiated.Format("2006-01-02 15:04:05")
			}
			fmt.Printf("%s  %s  uploadId=%s\n",
				initiated, c.S3Path(bucket, aws.ToString(u.Key)), aws.ToString(u.UploadId))
			count++
		}
	}
	if count == 0 {
		fmt.Printf("%s: no in-progress multipart uploads\n", c.S3Path(bucket, ""))
	}
	return nil
}

// MpuAbort 中止指定 uploadId，或一次性清空 prefix 下所有
func (c *S3Client) MpuAbort(bucket, prefix, uploadID string) error {
	// 单条指定 uploadId
	if uploadID != "" {
		_, err := c.S3.AbortMultipartUpload(c.Ctx, &s3.AbortMultipartUploadInput{
			Bucket: aws.String(bucket), Key: aws.String(prefix), UploadId: aws.String(uploadID),
		})
		if err != nil {
			return fmt.Errorf("abort mpu: %s", FormatAPIError(err))
		}
		fmt.Printf("aborted: %s  uploadId=%s\n", c.S3Path(bucket, prefix), uploadID)
		return nil
	}

	// 批量: 找到 prefix 下所有 in-progress, 全部 abort
	paginator := s3.NewListMultipartUploadsPaginator(c.S3, &s3.ListMultipartUploadsInput{Bucket: aws.String(bucket), Prefix: aws.String(prefix)})
	var aborted int
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(c.Ctx)
		if err != nil {
			return fmt.Errorf("list mpu: %s", FormatAPIError(err))
		}
		for _, u := range page.Uploads {
			_, err := c.S3.AbortMultipartUpload(c.Ctx, &s3.AbortMultipartUploadInput{
				Bucket: aws.String(bucket), Key: u.Key, UploadId: u.UploadId,
			})
			if err != nil {
				fmt.Printf("abort %s/%s: %s\n", bucket, aws.ToString(u.Key), FormatAPIError(err))
				continue
			}
			aborted++
		}
	}
	fmt.Printf("aborted %d in-progress uploads under s3://%s/%s\n", aborted, bucket, prefix)
	return nil
}
