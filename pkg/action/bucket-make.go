package action

import (
	"errors"
	"fmt"

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
func (c *S3Client) MakeBuckets(opt MakeBucketOptions, bucketname string) error {
	in := &s3.CreateBucketInput{Bucket: aws.String(bucketname)}

	if _, err := c.S3.CreateBucket(c.Ctx, in); err != nil {
		return err
	}

	// 收集 CORS / Policy / Lifecycle 设置错误，桶已创建成功时汇总返回
	var errs []error
	if opt.CorsFile != "" {
		if err := c.SetCors(opt.CorsFile, bucketname); err != nil {
			errs = append(errs, fmt.Errorf("set cors: %w", err))
		}
	}
	if opt.PolicyFile != "" {
		if err := c.SetPolicy(opt.PolicyFile, bucketname); err != nil {
			errs = append(errs, fmt.Errorf("set policy: %w", err))
		}
	}
	if opt.LifecycleFile != "" {
		if err := c.SetLifecycle(opt.LifecycleFile, bucketname); err != nil {
			errs = append(errs, fmt.Errorf("set lifecycle: %w", err))
		}
	}

	myprint.Successln("Bucket " + c.S3Path(bucketname, "") + " created")
	if len(errs) > 0 {
		return fmt.Errorf("bucket created but config errors: %w", errors.Join(errs...))
	}
	return nil
}
