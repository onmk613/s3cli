// bucket-versioning.go 实现桶版本控制状态管理: Set/GetBucketVersioning (Enabled / Suspended).

package action

import (
	"errors"
	"fmt"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/s3iface"
)

type VersioningOptions struct {
	Status string // Enabled / Suspended / Disabled
}

// SetVersioning 设置桶版本控制状态 (Enabled / Suspended / Disabled).
func (c *Action) SetVersioning(opt VersioningOptions, bucket string) error {
	if opt.Status == "" {
		return errors.New("status cannot be empty")
	}

	if err := c.S3.SetBucketVersioning(c.Ctx, bucket, s3iface.BucketVersioningStatus(opt.Status)); err != nil {
		return fmt.Errorf("set versioning %s: %s", bucket, FormatAPIError(err))
	}

	myprint.PrintfBoldGreen("Versioning for %s set to %s\n", c.S3Path(bucket, ""), opt.Status)
	return nil
}

// GetVersioning 查询并打印桶的版本控制状态; 从未启用时显示 (disabled).
func (c *Action) GetVersioning(bucket string) error {
	status, err := c.S3.GetBucketVersioning(c.Ctx, bucket)
	if err != nil {
		return fmt.Errorf("get versioning %s: %s", bucket, FormatAPIError(err))
	}
	s := string(status)
	if s == "" {
		s = "(disabled)"
	}

	myprint.PrintfBoldGreen("Versioning for %s: %s\n", c.S3Path(bucket, ""), s)
	return nil
}
