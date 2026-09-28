// bucket-public-access.go 实现桶级公共访问阻断 (Public Access Block) 配置管理:
// Get/Set/DelPublicAccessBlock.

package action

import (
	"errors"
	"fmt"
	"strings"

	"s3cli/internal/api"
	myprint "s3cli/internal/fmtutil"
	"s3cli/internal/i18n"
)

// PublicAccessBlockOptions 控制 `bucket public-access-block set` 的参数.
//
// 四个字段都是 *bool: nil 表示命令行未显式设置该项。api 层的配置结构是四个 bool,
// 因此未设置项在请求体中按 false 处理, 绝不会被 "顺带打开" —— 只有显式
// 给出 (含显式 =false) 的项才可能为 true。
type PublicAccessBlockOptions struct {
	BlockPublicACLs       *bool // --block-public-acls
	IgnorePublicACLs      *bool // --ignore-public-acls
	BlockPublicPolicy     *bool // --block-public-policy
	RestrictPublicBuckets *bool // --restrict-public-buckets
}

// GetPublicAccessBlock 打印桶的公共访问阻断配置 (JSON)。
func (c *Action) GetPublicAccessBlock(bucket string) error {
	cfg, err := c.S3.GetPublicAccessBlock(c.Ctx, bucket)
	if err != nil {
		return fmt.Errorf("get public access block %s: %w", bucket, err)
	}
	return c.printBucketConfigJSON(bucket, "public-access-block", cfg)
}

// SetPublicAccessBlock 设置桶的公共访问阻断配置 (至少显式给出一项)。
func (c *Action) SetPublicAccessBlock(opt PublicAccessBlockOptions, bucket string) error {
	cfg, err := buildPublicAccessBlockConfig(opt)
	if err != nil {
		return err
	}

	if err := c.S3.PutPublicAccessBlock(c.Ctx, bucket, cfg); err != nil {
		return fmt.Errorf("set public access block %s: %w", bucket, err)
	}

	myprint.PrintfBoldGreen(i18n.T("Public access block set for %s (%s)\n", "已为 %s 设置公共访问阻断（%s）\n"),
		c.S3Path(bucket, ""), describePublicAccessBlock(opt))
	return nil
}

// DelPublicAccessBlock 删除桶的公共访问阻断配置.
func (c *Action) DelPublicAccessBlock(bucket string) error {
	return c.deleteBucketConfig(bucket, "public access block", i18n.T("Public access block deleted for %s %s\n", "已为 %s %s 删除公共访问阻断配置\n"),
		func() error { return c.S3.DeletePublicAccessBlock(c.Ctx, bucket) })
}

// buildPublicAccessBlockConfig 把命令行参数收敛为 api.PublicAccessBlockConfiguration。
// 一项都没有显式设置时报错, 避免发出一个把四项全部置 false 的请求。
func buildPublicAccessBlockConfig(opt PublicAccessBlockOptions) (*api.PublicAccessBlockConfiguration, error) {
	if opt.BlockPublicACLs == nil && opt.IgnorePublicACLs == nil &&
		opt.BlockPublicPolicy == nil && opt.RestrictPublicBuckets == nil {
		return nil, errors.New(i18n.T(
			"public-access-block set: at least one of --block-public-acls / --ignore-public-acls / --block-public-policy / --restrict-public-buckets is required",
			"设置公共访问阻断：至少需要 --block-public-acls / --ignore-public-acls / --block-public-policy / --restrict-public-buckets 之一"))
	}
	return &api.PublicAccessBlockConfiguration{
		BlockPublicAcls:       boolValue(opt.BlockPublicACLs),
		IgnorePublicAcls:      boolValue(opt.IgnorePublicACLs),
		BlockPublicPolicy:     boolValue(opt.BlockPublicPolicy),
		RestrictPublicBuckets: boolValue(opt.RestrictPublicBuckets),
	}, nil
}

// boolValue 解引用可选的 bool: 未设置 (nil) 一律按 false 处理。
func boolValue(b *bool) bool { return b != nil && *b }

// describePublicAccessBlock 生成确认信息中的参数摘要 (只列出显式设置的项)。
func describePublicAccessBlock(opt PublicAccessBlockOptions) string {
	var parts []string
	add := func(name string, v *bool) {
		if v != nil {
			parts = append(parts, fmt.Sprintf("%s=%t", name, *v))
		}
	}
	add("block-public-acls", opt.BlockPublicACLs)
	add("ignore-public-acls", opt.IgnorePublicACLs)
	add("block-public-policy", opt.BlockPublicPolicy)
	add("restrict-public-buckets", opt.RestrictPublicBuckets)
	return strings.Join(parts, ", ")
}
