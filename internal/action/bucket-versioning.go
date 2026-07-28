// bucket-versioning.go 实现桶版本控制状态管理: Set/GetBucketVersioning (Enabled / Suspended).

package action

import (
	"errors"
	"fmt"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/s3api"
)

// SetVersioning 设置桶版本控制状态 (Enabled / Suspended).
func (c *S3Client) SetVersioning(bucket string, status string) error {
	if status == "" {
		return errors.New("status cannot be empty")
	}

	if err := c.S3.SetBucketVersioning(c.Ctx, bucket, s3api.BucketVersioningStatus(status)); err != nil {
		return fmt.Errorf("set versioning %s: %s", bucket, FormatAPIError(err))
	}

	myprint.PrintfBoldGreen("Versioning %s for %s %s\n", status, c.Alias, bucket)
	return nil
}

// GetVersioning 查询并打印桶的版本控制状态; 从未启用时显示 (disabled).
func (c *S3Client) GetVersioning(bucket string) error {
	status, err := c.S3.GetBucketVersioning(c.Ctx, bucket)
	if err != nil {
		return fmt.Errorf("get versioning %s: %s", bucket, FormatAPIError(err))
	}
	s := string(status)
	if s == "" {
		s = "(disabled)"
	}

	myprint.PrintfBoldGreen("Bucket %s versioning for %s: %s\n", c.Alias, bucket, s)
	return nil
}
