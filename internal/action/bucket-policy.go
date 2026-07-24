package action

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/s3api"
)

func (c *S3Client) SetPolicy(policyFile, bucket string) error {
	data, _, err := loadAWSConfigFile(policyFile)
	if err != nil {
		return err
	}
	if err := validateJSON(data); err != nil {
		return err
	}
	if err := c.S3.SetBucketPolicy(c.Ctx, bucket, data); err != nil {
		return FormatAPIError(err)
	}

	myprint.PrintfBoldGreen("Policy set for %s\n", c.S3Path(bucket, ""))
	return nil
}

func (c *S3Client) GetPolicy(bucket string) error {
	raw, err := c.S3.GetBucketPolicy(c.Ctx, bucket)
	if err != nil {
		return FormatAPIError(err)
	}
	myprint.PrintfBoldBlue("# %s policy:\n", c.S3Path(bucket, ""))

	var pretty bytes.Buffer
	if err := json.Indent(&pretty, raw, "", "  "); err == nil {
		myprint.PrintlnGreen(pretty.String())
	} else {
		myprint.PrintlnGreen(string(raw))
	}
	return nil
}

func (c *S3Client) DelPolicy(bucket string) error {
	if err := c.S3.DeleteBucketPolicy(c.Ctx, bucket); err != nil {
		return FormatAPIError(err)
	}

	myprint.PrintfBoldGreen("Policy deleted for %s: success\n", c.S3Path(bucket, ""))
	return nil
}

// cannedPolicyActions 预定义匿名 (*) 授权动作。
var cannedPolicyActions = map[string][]string{
	"public-read":       {"s3:GetObject"},
	"public-read-write": {"s3:GetObject", "s3:PutObject", "s3:DeleteObject"},
}

// buildCannedPolicy 构造最小化桶策略 JSON, 授予匿名 (*) 对指定资源的访问。
// prefix 为空时作用于整个桶 (bucket/*), 否则作用于 bucket/<prefix>*。
func buildCannedPolicy(name, bucket, prefix string) ([]byte, error) {
	actions, ok := cannedPolicyActions[name]
	if !ok {
		return nil, fmt.Errorf("unknown canned policy %q", name)
	}
	resource := "arn:aws:s3:::" + bucket + "/" + prefix + "*"

	// 单个 action 用字符串、多个用数组, 与 AWS 标准策略风格一致。
	var actionField interface{}
	if len(actions) == 1 {
		actionField = actions[0]
	} else {
		actionField = actions
	}
	sid := "PublicRead"
	if name == "public-read-write" {
		sid = "PublicReadWrite"
	}

	policy := map[string]interface{}{
		"Version": "2012-10-17",
		"Statement": []map[string]interface{}{
			{
				"Sid":       sid,
				"Effect":    "Allow",
				"Principal": "*",
				"Action":    actionField,
				"Resource":  resource,
			},
		},
	}
	return json.Marshal(policy)
}

// ApplyCannedPolicy 应用预定义策略 (public-read / public-read-write / private)。
// private 移除桶策略以恢复默认私有 (无策略时视为已私有, 保持幂等);
// 其余写入对应的匿名访问授权, prefix 非空时仅对该 key 前缀下的对象生效。
func (c *S3Client) ApplyCannedPolicy(name, bucket, prefix string) error {
	if name == "private" {
		if err := c.S3.DeleteBucketPolicy(c.Ctx, bucket); err != nil {
			// 无策略 (已私有) 视为成功, 保持幂等。
			var apiErr *s3api.ErrorResponse
			if !(errors.As(err, &apiErr) && (apiErr.Code == "NoSuchBucketPolicy" || apiErr.Code == "NoSuchBucket" || apiErr.Code == "404")) {
				return FormatAPIError(err)
			}
		}
		myprint.PrintfBoldGreen("Policy removed (private) for %s\n", c.S3Path(bucket, ""))
		return nil
	}

	data, err := buildCannedPolicy(name, bucket, prefix)
	if err != nil {
		return err
	}
	if err := c.S3.SetBucketPolicy(c.Ctx, bucket, data); err != nil {
		return FormatAPIError(err)
	}
	scope := "whole bucket"
	if prefix != "" {
		scope = "prefix " + prefix
	}
	myprint.PrintfBoldGreen("Policy %s set for %s (%s)\n", name, c.S3Path(bucket, ""), scope)
	return nil
}
