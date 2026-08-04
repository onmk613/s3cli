// bucket-make.go 实现桶创建 (MakeBuckets), 支持在创建的同时一次性套用
// CORS / Policy / Lifecycle / Versioning 配置.

package action

import (
	"errors"
	"fmt"

	myprint "s3cli/pkg/fmtutil"
	s3iface "s3cli/pkg/s3iface"
)

// 创建存储桶的同时设置cors、policy、lifecycle
type MakeBucketOptions struct {
	CorsFile       string
	PolicyFile     string
	LifecycleFile  string
	Versioning     bool
	Region         string // 桶所在区域 (mc mb --region)
	ObjectLocking  bool   // 启用对象锁 (mc mb --with-lock)
	IgnoreExisting bool   // 桶已存在时静默成功 (mc mb --ignore-existing)
}

// MakeBuckets 创建桶
//
// 桶创建成功后的各配置子步骤 (cors/policy/lifecycle/versioning/quota) 任一失败
// 都会聚合为错误返回 (桶本身仍在), 避免脚本依据退出码误判全部成功。
func (c *Action) MakeBuckets(opt MakeBucketOptions, bucket string) error {
	if err := c.S3.CreateBucket(c.Ctx, bucket, &s3iface.MakeBucketOptions{
		Region:        opt.Region,
		ObjectLocking: opt.ObjectLocking,
	}); err != nil {
		var apiErr *s3iface.ErrorResponse
		if opt.IgnoreExisting && errors.As(err, &apiErr) && (apiErr.Code == "BucketAlreadyOwnedByYou" || apiErr.Code == "BucketAlreadyExists") {
			myprint.PrintfDim("Bucket %s already exists (ignored)\n", bucket)
			return nil
		}
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
		step("cors", func() error { return c.SetCors(CorsOptions{ConfigFile: opt.CorsFile}, bucket) })
	}
	if opt.PolicyFile != "" {
		step("policy", func() error { return c.SetPolicy(PolicyOptions{ConfigFile: opt.PolicyFile}, bucket) })
	}
	if opt.LifecycleFile != "" {
		step("lifecycle", func() error { return c.SetLifecycle(LifecycleOptions{ConfigFile: opt.LifecycleFile}, bucket) })
	}
	if opt.Versioning {
		step("versioning", func() error { return c.SetVersioning(bucket, "Enabled") })
	}

	return errors.Join(errs...)
}
