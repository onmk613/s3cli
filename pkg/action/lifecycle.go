package action

import (
	"fmt"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/s3api/lifecycle"
)

// SetLifecycle 设置生命周期 (JSON, AWS CLI 兼容)
func (c *S3Client) SetLifecycle(lifecycleFile, bucket string) error {
	loaded, err := loadJSONConfig[lifecycle.Config](lifecycleFile, "lifecycle")
	if err != nil {
		return err
	}
	cfg := *loaded
	if len(cfg.Rules) == 0 {
		return fmt.Errorf("no lifecycle rules found in %s", lifecycleFile)
	}
	for i, r := range cfg.Rules {
		if r.Status == "" {
			return fmt.Errorf("rule[%d] missing required field 'Status' (Enabled/Disabled)", i)
		}
		if r.Filter == nil {
			return fmt.Errorf("rule[%d] must specify 'Filter' or legacy 'Prefix'", i)
		}
	}

	if err := c.S3.SetBucketLifecycle(c.Ctx, bucket, &cfg); err != nil {
		return fmt.Errorf("set lifecycle %s: %s", bucket, FormatAPIError(err))
	}

	myprint.PrintfBoldGreen("Lifecycle set for %s %s (%d rules)\n", c.Alias, bucket, len(cfg.Rules))
	return nil
}

// GetLifecycle 打印生命周期
func (c *S3Client) GetLifecycle(bucket string) error {
	cfg, err := c.S3.GetBucketLifecycle(c.Ctx, bucket)
	if err != nil {
		return fmt.Errorf("get lifecycle %s: %s", bucket, FormatAPIError(err))
	}
	return c.printBucketConfigJSON(bucket, "lifecycle:", cfg)
}

// DelLifecycle 删除生命周期
func (c *S3Client) DelLifecycle(bucket string) error {
	return c.deleteBucketConfig(bucket, "lifecycle", "Lifecycle deleted for %s %s\n",
		func() error { return c.S3.DeleteBucketLifecycle(c.Ctx, bucket) })
}
