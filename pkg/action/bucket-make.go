package action

import (
	myprint "s3cli/pkg/fmtutil"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// 创建存储桶的同时设置cors、policy、lifecycle
type MakeBucketOptions struct {
	CorsFile      string
	PolicyFile    string
	LifecycleFile string
}

// Mb 创建桶
func (c *S3Client) MakeBuckets(opt MakeBucketOptions, bucket string) error {
	if _, err := c.S3.CreateBucket(c.Ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)}); err != nil {
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

	return nil
}
