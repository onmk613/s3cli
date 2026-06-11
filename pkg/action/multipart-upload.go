package action

import (
	"fmt"

	myprint "s3cli/pkg/fmtutil"

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
			myprint.Printf("%s  %s  uploadId=%s\n",
				initiated, c.S3Path(bucket, aws.ToString(u.Key)), aws.ToString(u.UploadId))
			count++
		}
	}
	if count == 0 {
		myprint.PrintfYellow("%s: no in-progress multipart uploads\n", c.S3Path(bucket, ""))
	}
	return nil
}

// MpuAbort 中止指定 uploadId，或一次性清空 prefix 下所有
func (c *S3Client) MpuAbort(bucket, prefix, uploadID string) error {
	// 单条指定 uploadId
	if uploadID != "" {
		// AbortMultipartUpload 需要 (Bucket, Key, UploadId) 三元组。
		// 若用户未提供具体的 object key（prefix 为空），尝试通过列举
		// 该 prefix 下的 in-progress uploads，找到匹配该 uploadId 的真实 key。
		key := prefix
		if key == "" {
			found, err := c.findUploadKey(bucket, prefix, uploadID)
			if err != nil {
				return err
			}
			if found == "" {
				return fmt.Errorf("abort mpu: object key is required for uploadId %q "+
					"(no matching in-progress upload found under %s); "+
					"run `mpu list` to find the key", uploadID, c.S3Path(bucket, prefix))
			}
			key = found
		}

		_, err := c.S3.AbortMultipartUpload(c.Ctx, &s3.AbortMultipartUploadInput{
			Bucket: aws.String(bucket), Key: aws.String(key), UploadId: aws.String(uploadID),
		})
		if err != nil {
			return fmt.Errorf("abort mpu: %s", FormatAPIError(err))
		}

		myprint.PrintfGreen("aborted: %s  uploadId=%s\n", c.S3Path(bucket, key), uploadID)
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
				myprint.PrintfRed("abort %s/%s: %s\n", bucket, aws.ToString(u.Key), FormatAPIError(err))
				continue
			}
			aborted++
		}
	}
	myprint.PrintfGreen("aborted %d in-progress uploads under s3://%s/%s\n", aborted, bucket, prefix)
	return nil
}

// findUploadKey 在 prefix 下列举 in-progress multipart uploads，
// 返回与给定 uploadID 匹配的对象 key；找不到时返回空字符串。
func (c *S3Client) findUploadKey(bucket, prefix, uploadID string) (string, error) {
	paginator := s3.NewListMultipartUploadsPaginator(c.S3, &s3.ListMultipartUploadsInput{
		Bucket: aws.String(bucket), Prefix: aws.String(prefix),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(c.Ctx)
		if err != nil {
			return "", fmt.Errorf("list mpu: %s", FormatAPIError(err))
		}
		for _, u := range page.Uploads {
			if aws.ToString(u.UploadId) == uploadID {
				return aws.ToString(u.Key), nil
			}
		}
	}
	return "", nil
}
