// bucket-replication.go 实现桶级复制 (Replication) 配置管理:
// Get/Set/DelBucketReplication, 配置从 AWS CLI 兼容的 JSON 文件加载.

package action

import (
	"errors"
	"fmt"
	"strings"

	"s3cli/internal/api"
	myprint "s3cli/internal/fmtutil"
	"s3cli/internal/i18n"
)

// ReplicationOptions 控制 `bucket replication set` 的参数.
type ReplicationOptions struct {
	ConfigFile string // --from-file: AWS CLI 兼容的 JSON 配置
}

// GetReplication 打印桶的复制配置 (JSON)。
func (c *Action) GetReplication(bucket string) error {
	cfg, err := c.S3.GetBucketReplication(c.Ctx, bucket)
	if err != nil {
		return fmt.Errorf("get replication %s: %w", bucket, err)
	}
	return c.printBucketConfigJSON(bucket, "replication", cfg)
}

// SetReplication 从 JSON 文件设置桶的复制配置.
func (c *Action) SetReplication(opt ReplicationOptions, bucket string) error {
	if strings.TrimSpace(opt.ConfigFile) == "" {
		return errors.New(i18n.T("replication set: --from-file is required", "设置复制：必须指定 --from-file"))
	}
	cfg, err := loadJSONConfig[api.ReplicationConfiguration](opt.ConfigFile, "replication")
	if err != nil {
		return err
	}
	// JSON 解析对未识别键宽容 (见 unmarshalAWS): 键名拼错只会留下空的 Rules,
	// 发给服务端只会换回一个难以定位的 MalformedXML。
	if len(cfg.Rules) == 0 {
		return fmt.Errorf(i18n.T("no replication rules found in %s (expected Role / Rules)", "在 %s 中未找到复制规则（应为 Role / Rules）"), opt.ConfigFile)
	}

	if err := c.S3.PutBucketReplication(c.Ctx, bucket, cfg); err != nil {
		return fmt.Errorf("set replication %s: %w", bucket, err)
	}

	myprint.PrintfBoldGreen(i18n.T("Replication configuration set for %s %s (%d rules)\n", "已为 %s %s 设置复制配置（%d 条规则）\n"), c.Alias, bucket, len(cfg.Rules))
	return nil
}

// DelReplication 删除桶的复制配置.
func (c *Action) DelReplication(bucket string) error {
	return c.deleteBucketConfig(bucket, "replication", i18n.T("Replication configuration deleted for %s %s\n", "已为 %s %s 删除复制配置\n"),
		func() error { return c.S3.DeleteBucketReplication(c.Ctx, bucket) })
}
