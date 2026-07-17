package action

import (
	"fmt"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/s3api"
)

// MpuList 列出未完成的 multipart upload
func (c *S3Client) MpuList(bucket, prefix string) error {
	out, err := c.S3.ListMultipartUploads(c.Ctx, bucket, &s3api.ListMultipartUploadsOptions{
		Prefix: prefix,
	})
	if err != nil {
		return fmt.Errorf("list multipart uploads: %s", FormatAPIError(err))
	}
	var count int
	for _, u := range out.Uploads {
		initiated := ""
		if !u.Initiated.IsZero() {
			initiated = u.Initiated.Format("2006-01-02 15:04:05")
		}
		myprint.Printf("%s  %s  uploadId=%s\n",
			initiated, c.S3Path(bucket, u.Key), u.UploadID)
		count++
	}
	if count == 0 {
		myprint.PrintfBoldYellow("%s: no in-progress multipart uploads\n", c.S3Path(bucket, ""))
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

		if err := c.S3.AbortMultipartUpload(c.Ctx, bucket, key, uploadID); err != nil {
			return fmt.Errorf("abort mpu: %s", FormatAPIError(err))
		}

		myprint.PrintfGreen("aborted: %s  uploadId=%s\n", c.S3Path(bucket, key), uploadID)
		return nil
	}

	// 批量: 找到 prefix 下所有 in-progress, 全部 abort
	out, err := c.S3.ListMultipartUploads(c.Ctx, bucket, &s3api.ListMultipartUploadsOptions{Prefix: prefix})
	if err != nil {
		return fmt.Errorf("list mpu: %s", FormatAPIError(err))
	}
	var aborted int
	for _, u := range out.Uploads {
		if err := c.S3.AbortMultipartUpload(c.Ctx, bucket, u.Key, u.UploadID); err != nil {
			myprint.PrintfRed("abort %s/%s: %s\n", bucket, u.Key, FormatAPIError(err))
			continue
		}
		aborted++
	}
	myprint.PrintfBoldGreen("aborted %d in-progress uploads under %s\n", aborted, c.S3Path(bucket, prefix))
	return nil
}

// findUploadKey 在 prefix 下列举 in-progress multipart uploads，
func (c *S3Client) findUploadKey(bucket, prefix, uploadID string) (string, error) {
	out, err := c.S3.ListMultipartUploads(c.Ctx, bucket, &s3api.ListMultipartUploadsOptions{
		Prefix: prefix,
	})
	if err != nil {
		return "", fmt.Errorf("list mpu: %s", FormatAPIError(err))
	}
	for _, u := range out.Uploads {
		if u.UploadID == uploadID {
			return u.Key, nil
		}
	}
	return "", nil
}
