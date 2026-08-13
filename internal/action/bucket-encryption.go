// bucket-encryption.go 实现桶默认服务端加密配置管理:
// Set/Get/DelEncryption, 支持命令行参数或 AWS CLI 兼容 JSON 文件两种输入方式.

package action

import (
	"errors"
	"fmt"
	"strings"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/i18n"
	"s3cli/pkg/s3iface"
)

// EncryptionOptions set encryption 命令参数
type EncryptionOptions struct {
	// Algorithm: AES256 (SSE-S3) 或 aws:kms (SSE-KMS)
	Algorithm  string
	KMSKeyID   string // 当 Algorithm = aws:kms 时使用
	BucketKey  bool   // 是否启用 S3 Bucket Key (仅对 aws:kms 有效)
	ConfigFile string // 直接提供 AWS CLI 兼容的 JSON 配置, 覆盖上面字段
}

// SetEncryption 设置 bucket 默认加密
func (c *Action) SetEncryption(opt EncryptionOptions, bucket string) error {
	var cfg s3iface.ServerSideEncryptionConfiguration

	if opt.ConfigFile != "" {
		loaded, err := loadJSONConfig[s3iface.ServerSideEncryptionConfiguration](opt.ConfigFile, "encryption")
		if err != nil {
			return err
		}
		cfg = *loaded
	} else {
		algo := strings.TrimSpace(opt.Algorithm)
		if algo == "" {
			algo = "AES256"
		}
		rule := s3iface.ServerSideEncryptionRule{
			ApplyServerSideEncryptionByDefault: s3iface.ServerSideEncryptionByDefault{
				SSEAlgorithm: algo,
			},
		}
		if algo == "aws:kms" || algo == "aws:kms:dsse" {
			if opt.KMSKeyID == "" {
				return fmt.Errorf(i18n.T("--kms-key-id is required when --algorithm is %s", "--algorithm 为 %s 时必须指定 --kms-key-id"), algo)
			}
			rule.ApplyServerSideEncryptionByDefault.KMSMasterKeyID = opt.KMSKeyID
			if opt.BucketKey {
				bk := true
				rule.BucketKeyEnabled = &bk
			}
		}
		cfg.Rules = []s3iface.ServerSideEncryptionRule{rule}
	}

	if len(cfg.Rules) == 0 {
		return errors.New(i18n.T("no encryption rules configured", "未配置加密规则"))
	}

	if err := c.S3.SetBucketEncryption(c.Ctx, bucket, &cfg); err != nil {
		return fmt.Errorf("set encryption %s: %s", bucket, FormatAPIError(err))
	}

	myprint.PrintfBoldGreen(i18n.T("Encryption set for %s %s (%d rules)\n", "已为 %s %s 设置加密（%d 条规则）\n"), c.Alias, bucket, len(cfg.Rules))
	return nil
}

// GetEncryption 打印 bucket 默认加密 (JSON)
func (c *Action) GetEncryption(bucket string) error {
	cfg, err := c.S3.GetBucketEncryption(c.Ctx, bucket)
	if err != nil {
		return fmt.Errorf("get encryption %s: %s", bucket, FormatAPIError(err))
	}
	return c.printBucketConfigJSON(bucket, "encryption", cfg)
}

// DelEncryption 删除 bucket 默认加密配置
func (c *Action) DelEncryption(bucket string) error {
	return c.deleteBucketConfig(bucket, "encryption", i18n.T("Encryption deleted for %s %s\n", "已为 %s %s 删除加密配置\n"),
		func() error { return c.S3.DeleteBucketEncryption(c.Ctx, bucket) })
}
