package action

import (
	"fmt"
	"strings"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/s3api"
)

// EncryptionOptions setencryption 命令参数
type EncryptionOptions struct {
	// Algorithm: AES256 (SSE-S3) 或 aws:kms (SSE-KMS)
	Algorithm  string
	KMSKeyID   string // 当 Algorithm = aws:kms 时使用
	BucketKey  bool   // 是否启用 S3 Bucket Key (仅对 aws:kms 有效)
	ConfigFile string // 直接提供 AWS CLI 兼容的 JSON 配置, 覆盖上面字段
}

// SetEncryption 设置 bucket 默认加密
func (c *S3Client) SetEncryption(opt EncryptionOptions, bucket string) error {
	var cfg s3api.ServerSideEncryptionConfiguration

	if opt.ConfigFile != "" {
		loaded, err := loadJSONConfig[s3api.ServerSideEncryptionConfiguration](opt.ConfigFile, "encryption")
		if err != nil {
			return err
		}
		cfg = *loaded
	} else {
		algo := strings.TrimSpace(opt.Algorithm)
		if algo == "" {
			algo = "AES256"
		}
		rule := s3api.ServerSideEncryptionRule{
			ApplyServerSideEncryptionByDefault: s3api.ServerSideEncryptionByDefault{
				SSEAlgorithm: algo,
			},
		}
		if algo == "aws:kms" || algo == "aws:kms:dsse" {
			if opt.KMSKeyID == "" {
				return fmt.Errorf("--kms-key-id is required when --algorithm is %s", algo)
			}
			rule.ApplyServerSideEncryptionByDefault.KMSMasterKeyID = opt.KMSKeyID
			if opt.BucketKey {
				bk := true
				rule.BucketKeyEnabled = &bk
			}
		}
		cfg.Rules = []s3api.ServerSideEncryptionRule{rule}
	}

	if len(cfg.Rules) == 0 {
		return fmt.Errorf("no encryption rules configured")
	}

	if err := c.S3.SetBucketEncryption(c.Ctx, bucket, &cfg); err != nil {
		return fmt.Errorf("set encryption %s: %s", bucket, FormatAPIError(err))
	}

	myprint.PrintfBoldGreen("Encryption set for %s %s (%d rules)\n", c.Alias, bucket, len(cfg.Rules))
	return nil
}

// GetEncryption 打印 bucket 默认加密 (JSON)
func (c *S3Client) GetEncryption(bucket string) error {
	cfg, err := c.S3.GetBucketEncryption(c.Ctx, bucket)
	if err != nil {
		return fmt.Errorf("get encryption %s: %s", bucket, FormatAPIError(err))
	}
	return c.printBucketConfigJSON(bucket, "encryption", cfg)
}

// DelEncryption 删除 bucket 默认加密配置
func (c *S3Client) DelEncryption(bucket string) error {
	return c.deleteBucketConfig(bucket, "encryption", "Encryption deleted for %s %s\n",
		func() error { return c.S3.DeleteBucketEncryption(c.Ctx, bucket) })
}
