package action

import (
	"bytes"
	"fmt"

	myprint "s3cli/pkg/fmtutil"
	s3 "s3cli/pkg/s3api"
)

// SetLifecycle 设置生命周期 (本地文件支持 JSON 或 XML，自动识别)
func (c *S3Client) SetLifecycle(lifecycleFile, bucket string) error {
	data, format, err := loadAWSConfigFile(lifecycleFile)
	if err != nil {
		return err
	}

	cfg, err := parseLifecycleConfig(data, format)
	if err != nil {
		return fmt.Errorf("parse lifecycle file %s: %w", lifecycleFile, err)
	}

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

	if err := c.S3.SetBucketLifecycle(c.Ctx, bucket, cfg); err != nil {
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

// parseLifecycleConfig 解析生命周期配置文件，支持 JSON 和 XML 格式。
func parseLifecycleConfig(data []byte, format string) (*s3.LifecycleConfig, error) {
	switch format {
	case "json":
		var c s3.LifecycleConfig
		if err := unmarshalAWS(data, "json", &c); err != nil {
			return nil, err
		}
		return &c, nil
	case "xml":
		return s3.ParseBucketLifecycleConfig(bytes.NewReader(data))
	}
	return nil, fmt.Errorf("unknown format %q", format)
}
