package action

import (
	"errors"
	"fmt"

	myprint "s3cli/pkg/fmtutil"
)

// 创建存储桶的同时设置cors、policy、lifecycle
type MakeBucketOptions struct {
	CorsFile      string
	PolicyFile    string
	LifecycleFile string
	Versioning    bool
}

// MakeBuckets 创建桶
//
// 桶创建成功后的各配置子步骤 (cors/policy/lifecycle/versioning/quota) 任一失败
// 都会聚合为错误返回 (桶本身仍在), 避免脚本依据退出码误判全部成功。
func (c *S3Client) MakeBuckets(opt MakeBucketOptions, bucket string) error {
	if err := c.S3.CreateBucket(c.Ctx, bucket, nil); err != nil {
		return err
	}
	myprint.PrintfBoldGreen("Bucket %s created for %s\n", bucket, c.Alias)

	// 配置 CORS / Policy / Lifecycle / Versioning / Quota
	var errs []error
	step := func(name string, fn func() error) {
		if err := fn(); err != nil {
			myprint.PrintfBoldYellow("set %s: %v\n", name, err)
			errs = append(errs, fmt.Errorf("set %s: %w", name, err))
		} else {
			myprint.PrintlnBoldGreen("set " + name + " success")
		}
	}

	if opt.CorsFile != "" {
		step("cors", func() error { return c.SetCors(opt.CorsFile, bucket) })
	}
	if opt.PolicyFile != "" {
		step("policy", func() error { return c.SetPolicy(opt.PolicyFile, bucket) })
	}
	if opt.LifecycleFile != "" {
		step("lifecycle", func() error { return c.SetLifecycle(opt.LifecycleFile, bucket) })
	}
	if opt.Versioning {
		step("versioning", func() error { return c.SetVersioning(bucket, "Enabled") })
	}

	return errors.Join(errs...)
}
