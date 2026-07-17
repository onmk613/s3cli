package client

import (
	"fmt"
	"net/url"
	"strings"
)

// CustomBucketLookup 实现基于占位符模板的 bucket 自定义寻址。
// 模板用 %(bucket) 表示 bucket 名, 用可选的 %(region) 表示 bucket 所在区域。
type CustomBucketLookup struct {
	Template          string
	BucketPlaceholder string
	RegionPlaceholder string
}

// NeedsRegion 报告模板是否引用了 region 占位符。
// 为 true 时, Client 会通过 GetBucketLocation 探测 bucket 的实际 region 并注入模板。
func (c *CustomBucketLookup) NeedsRegion() bool {
	return c.RegionPlaceholder != "" && strings.Contains(c.Template, c.RegionPlaceholder)
}

// ResolveCustomEndpoint 使用一个含 %(bucket) (及可选 %(region)) 占位符的模板,
// 把 bucket 名 (和 region) 替换进去, 得到最终的 endpoint URL.
//
// 例如:
//
//	Template          = "https://%(bucket).s3.%(region).example.com"
//	BucketPlaceholder = "%(bucket)"
//	RegionPlaceholder = "%(region)"
//	bucket            = "mydata"
//	region            = "us-west-2"
//	->                  https://mydata.s3.us-west-2.example.com
func (c *CustomBucketLookup) ResolveCustomEndpoint(bucket, region string) (*url.URL, error) {
	template := c.Template
	if template == "" {
		return nil, fmt.Errorf("custom endpoint template is empty")
	}
	if bucket == "" {
		return nil, fmt.Errorf("bucket is required for custom addressing")
	}

	raw := strings.ReplaceAll(template, c.BucketPlaceholder, bucket)
	if c.RegionPlaceholder != "" {
		raw = strings.ReplaceAll(raw, c.RegionPlaceholder, region)
	}

	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse custom endpoint %q: %w", raw, err)
	}

	if u.Host == "" {
		return nil, fmt.Errorf("custom endpoint %q has empty host", raw)
	}

	return u, nil
}
