package action

import (
	"encoding/json"
	"fmt"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/utils"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// SetLifecycle 设置生命周期 (JSON, AWS CLI 兼容)
func (c *S3Client) SetLifecycle(lifecyclefile, bucket string) error {
	data, format, err := utils.LoadAWSConfigFile(lifecyclefile)
	if err != nil {
		return err
	}
	if format != "json" {
		return fmt.Errorf("lifecycle only supports JSON format (AWS CLI compatible)")
	}

	var cfg s3types.BucketLifecycleConfiguration
	if err := utils.UnmarshalAWS(data, "json", &cfg); err != nil {
		return fmt.Errorf("parse lifecycle file %s: %w", lifecyclefile, err)
	}
	if len(cfg.Rules) == 0 {
		return fmt.Errorf("no lifecycle rules found in %s", lifecyclefile)
	}
	for i, r := range cfg.Rules {
		if r.Status == "" {
			return fmt.Errorf("rule[%d] missing required field 'Status' (Enabled/Disabled)", i)
		}
		if r.Filter == nil {
			return fmt.Errorf("rule[%d] must specify 'Filter' or legacy 'Prefix'", i)
		}
	}

	_, err = c.S3.PutBucketLifecycleConfiguration(c.Ctx, &s3.PutBucketLifecycleConfigurationInput{
		Bucket: aws.String(bucket),
		LifecycleConfiguration: &cfg,
	})
	if err != nil {
		return fmt.Errorf("set lifecycle %s: %s", bucket, FormatAPIError(err))
	}

	myprint.PrintfBoldGreen("Lifecycle set for %s %s (%d rules)\n", c.Alias, bucket, len(cfg.Rules))
	return nil
}

// GetLifecycle 打印生命周期
func (c *S3Client) GetLifecycle(bucket string) error {
	out, err := c.S3.GetBucketLifecycleConfiguration(c.Ctx, &s3.GetBucketLifecycleConfigurationInput{Bucket: aws.String(bucket)})
	if err != nil {
		return fmt.Errorf("get lifecycle %s: %s", bucket, FormatAPIError(err))
	}
	b, err := json.MarshalIndent(map[string]any{"Rules": out.Rules}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal lifecycle: %w", err)
	}

	myprint.PrintfBoldBlue("# %s %s lifecycle:\n", c.Alias, bucket)
	myprint.PrintlnGreen(string(b))
	return nil
}

// DelLifecycle 删除生命周期
func (c *S3Client) DelLifecycle(bucket string) error {
	if _, err := c.S3.DeleteBucketLifecycle(c.Ctx, &s3.DeleteBucketLifecycleInput{Bucket: aws.String(bucket)}); err != nil {
		return fmt.Errorf("delete lifecycle %s: %s", bucket, FormatAPIError(err))
	}

	myprint.PrintfBoldGreen("Lifecycle deleted for %s %s\n", c.Alias, bucket)
	return nil
}
