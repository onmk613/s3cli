package action

import (
	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/utils"
)

// 创建存储桶的同时设置cors、policy、lifecycle
type MakeBucketOptions struct {
	CorsFile      string
	PolicyFile    string
	LifecycleFile string
	Versioning    bool
	Quota         string
}

// MakeBuckets 创建桶
func (c *S3Client) MakeBuckets(opt MakeBucketOptions, bucket string) error {
	if err := c.S3.CreateBucket(c.Ctx, bucket, nil); err != nil {
		return err
	}
	myprint.PrintfBoldGreen("Bucket %s created for %s\n", bucket, c.Alias)

	// 配置 CORS / Policy / Lifecycle
	if opt.CorsFile != "" {
		if err := c.SetCors(opt.CorsFile, bucket); err != nil {
			myprint.PrintfBoldYellow("set cors: %v", err)
		} else {
			myprint.PrintlnBoldGreen("set cors success")
		}
	}

	if opt.PolicyFile != "" {
		if err := c.SetPolicy(opt.PolicyFile, bucket); err != nil {
			myprint.PrintfBoldYellow("set policy: %v", err)
		} else {
			myprint.PrintlnBoldGreen("set policy success")
		}
	}

	if opt.LifecycleFile != "" {
		if err := c.SetLifecycle(opt.LifecycleFile, bucket); err != nil {
			myprint.PrintfBoldYellow("set lifecycle: %v", err)
		} else {
			myprint.PrintlnBoldGreen("set lifecycle success")
		}
	}

	if opt.Versioning {
		if err := c.SetVersioning(bucket, "Enabled"); err != nil {
			myprint.PrintfBoldYellow("set versioning: %v", err)
		} else {
			myprint.PrintlnBoldGreen("set versioning success")
		}
	}

	if opt.Quota != "" {
		if err := c.SetBucketQuota(bucket, opt.Quota); err != nil {
			myprint.PrintfBoldYellow("set bucket quota: %v", err)
		} else {
			myprint.PrintlnBoldGreen("set bucket quota success")
		}
	}

	return nil
}

func (c *S3Client) SetBucketQuota(bucket string, quota string) error {
	quotaInt, err := utils.ParseBytes(quota)
	if err != nil {
		return err
	}
	if err := c.S3.SetBucketQuota(c.Ctx, bucket, quotaInt); err != nil {
		return err
	}
	myprint.PrintfBoldGreen("set bucket %s quota to %s success\n", bucket, quota)
	return nil
}

func (c *S3Client) InfoBucketQuota(bucket string) error {
	quotaInt, err := c.S3.InfoBucketQuota(c.Ctx, bucket)
	if err != nil {
		return err
	}
	quota := utils.FormatBytes(quotaInt)
	myprint.PrintfBoldGreen("bucket %s quota: %s\n", bucket, quota)
	return nil
}
