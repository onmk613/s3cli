package action

import (
	"encoding/json"
	"fmt"
	"strings"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/utils"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
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
	var cfg s3types.ServerSideEncryptionConfiguration

	if opt.ConfigFile != "" {
		data, format, err := utils.LoadAWSConfigFile(opt.ConfigFile)
		if err != nil {
			return err
		}
		if format != "json" {
			return fmt.Errorf("encryption only supports JSON format (AWS CLI compatible)")
		}
		if err := utils.UnmarshalAWS(data, "json", &cfg); err != nil {
			return fmt.Errorf("parse encryption file %s: %w", opt.ConfigFile, err)
		}
	} else {
		algo := strings.TrimSpace(opt.Algorithm)
		if algo == "" {
			algo = "AES256"
		}
		rule := s3types.ServerSideEncryptionRule{
			ApplyServerSideEncryptionByDefault: &s3types.ServerSideEncryptionByDefault{
				SSEAlgorithm: s3types.ServerSideEncryption(algo),
			},
		}
		if algo == "aws:kms" || algo == "aws:kms:dsse" {
			if opt.KMSKeyID == "" {
				return fmt.Errorf("--kms-key-id is required when --algorithm is %s", algo)
			}
			rule.ApplyServerSideEncryptionByDefault.KMSMasterKeyID = aws.String(opt.KMSKeyID)
			if opt.BucketKey {
				rule.BucketKeyEnabled = aws.Bool(true)
			}
		}
		cfg.Rules = []s3types.ServerSideEncryptionRule{rule}
	}

	if len(cfg.Rules) == 0 {
		return fmt.Errorf("no encryption rules configured")
	}

	_, err := c.S3.PutBucketEncryption(c.Ctx, &s3.PutBucketEncryptionInput{
		Bucket:                            aws.String(bucket),
		ServerSideEncryptionConfiguration: &cfg,
	})
	if err != nil {
		return fmt.Errorf("set encryption %s: %s", bucket, FormatAPIError(err))
	}

	myprint.Info("set encryption: bucket=%s rules=%d", bucket, len(cfg.Rules))
	myprint.Successf("Encryption set for %s (%d rules)\n", c.S3Path(bucket, ""), len(cfg.Rules))
	return nil
}

// GetEncryption 打印 bucket 默认加密 (JSON)
func (c *S3Client) GetEncryption(bucket string) error {
	out, err := c.S3.GetBucketEncryption(c.Ctx, &s3.GetBucketEncryptionInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		return fmt.Errorf("get encryption %s: %s", bucket, FormatAPIError(err))
	}
	b, err := json.MarshalIndent(out.ServerSideEncryptionConfiguration, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal encryption: %w", err)
	}

	myprint.PrintfDim("# %s encryption\n", c.S3Path(bucket, ""))
	myprint.Println(string(b))
	return nil
}

// DelEncryption 删除 bucket 默认加密配置
func (c *S3Client) DelEncryption(bucket string) error {
	_, err := c.S3.DeleteBucketEncryption(c.Ctx, &s3.DeleteBucketEncryptionInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		return fmt.Errorf("delete encryption %s: %s", bucket, FormatAPIError(err))
	}

	myprint.Info("delete encryption: bucket=%s", bucket)
	myprint.Successf("Encryption deleted for %s\n", c.S3Path(bucket, ""))
	return nil
}
