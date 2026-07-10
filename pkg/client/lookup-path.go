package client

import (
	"fmt"
	"net/url"
	"strings"
)

// ResolveCustomEndpoint 使用一个含 %(bucket) 占位符的模板,
// 把 bucket 名替换进去, 得到最终的 endpoint URL.
//
// 例如:
//
//		Template = "https://www.%(bucket).example.com"
//	    BucketPlaceholder = "%(bucket)"
//		bucket   = "mydata"
//		->         https://www.mydata.example.com

type CustomBucketLookup struct {
	Template          string
	BucketPlaceholder string
}

func (c *CustomBucketLookup) ResolveCustomEndpoint(bucket string) (*url.URL, error) {
	template := c.Template
	placeholder := c.BucketPlaceholder
	if template == "" {
		return nil, fmt.Errorf("custom endpoint template is empty")
	}
	if bucket == "" {
		return nil, fmt.Errorf("bucket is required for custom addressing")
	}

	raw := strings.ReplaceAll(template, placeholder, bucket)

	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse custom endpoint %q: %w", raw, err)
	}

	if u.Host == "" {
		return nil, fmt.Errorf("custom endpoint %q has empty host", raw)
	}

	return u, nil
}
