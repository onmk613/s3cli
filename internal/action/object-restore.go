// object-restore.go 实现 restore 命令的动作: 请求从归档存储类
// (GLACIER / DEEP_ARCHIVE 等) 恢复对象的可访问副本.

package action

import (
	"errors"
	"fmt"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/i18n"
	"s3cli/pkg/s3iface"
)

// RestoreOptions 控制 restore 命令的参数.
type RestoreOptions struct {
	Days      int    // 恢复后保持可访问的天数
	Tier      string // 恢复层级: Expedited / Standard / Bulk
	VersionID string // 指定对象版本
}

// Restore 请求恢复归档对象.
func (c *Action) Restore(opt RestoreOptions, bucket, key string) error {
	if key == "" {
		return errors.New(i18n.T("restore requires an object key, not a bare bucket", "restore 需要指定对象 key，而非裸存储桶"))
	}
	if opt.Days <= 0 {
		opt.Days = 1
	}
	req := &s3iface.RestoreRequest{Days: opt.Days}
	if opt.Tier != "" {
		req.GlacierJobParameters = &s3iface.GlacierJobParameters{Tier: opt.Tier}
	}

	if err := c.S3.RestoreObject(c.Ctx, bucket, key, opt.VersionID, req); err != nil {
		return fmt.Errorf("restore %s: %s", c.S3Path(bucket, key), FormatAPIError(err))
	}

	tier := opt.Tier
	if tier == "" {
		tier = "Standard"
	}
	myprint.PrintfBoldGreen(i18n.T("Restore initiated for %s (days=%d, tier=%s)\n", "已为 %s 发起恢复（天数=%d，层级=%s）\n"), c.S3Path(bucket, key), opt.Days, tier)
	return nil
}
