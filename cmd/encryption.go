package cmd

import (
	"s3cli/internal/action"
	"s3cli/internal/s3path"
	"s3cli/pkg/i18n"

	"github.com/spf13/cobra"
)

// EncryptionCmd 管理桶的默认加密配置 (SSE-S3 / SSE-KMS)
func EncryptionCmd() *cobra.Command {
	encryptionCmd := &cobra.Command{
		Use:   "encryption",
		Short: i18n.T("Manage bucket default encryption (SSE-S3 / SSE-KMS)", "管理存储桶默认加密（SSE-S3 / SSE-KMS）"),
	}
	encryptionCmd.AddCommand(EncryptionSetCmd(), EncryptionGetCmd(), EncryptionDelCmd())
	return encryptionCmd
}

func EncryptionSetCmd() *cobra.Command {
	var encOpt action.EncryptionOptions
	cmd := &cobra.Command{
		Use:               "set [alias:bucket] ...",
		Short:             i18n.T("Set bucket default encryption (SSE-S3 / SSE-KMS)", "设置存储桶默认加密（SSE-S3 / SSE-KMS）"),
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.SetEncryption(encOpt, dst.Bucket)
		}),
	}

	cmd.Flags().StringVar(&encOpt.Algorithm, "algorithm", "AES256", i18n.T("Encryption algorithm: AES256 / aws:kms", "加密算法：AES256 / aws:kms"))
	cmd.Flags().StringVar(&encOpt.KMSKeyID, "kms-key-id", "", i18n.T("KMS key id (required for aws:kms)", "KMS key id（aws:kms 必填）"))
	cmd.Flags().BoolVar(&encOpt.BucketKey, "bucket-key", false, i18n.T("Enable S3 Bucket Key (aws:kms only)", "启用 S3 Bucket Key（仅 aws:kms）"))
	cmd.Flags().StringVar(&encOpt.ConfigFile, "from-file", "", i18n.T("Load AWS CLI JSON config instead of using flags", "从 AWS CLI JSON 配置加载，而非使用标志"))
	return cmd
}

func EncryptionGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "get [alias:bucket] ...",
		Short:             i18n.T("Print bucket(s) default encryption configuration (JSON)", "打印存储桶默认加密配置（JSON）"),
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.GetEncryption(dst.Bucket)
		}),
	}
}

func EncryptionDelCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "del [alias:bucket] ...",
		Short:             i18n.T("Delete bucket(s) default encryption configuration", "删除存储桶默认加密配置"),
		ValidArgsFunction: AutoCompleteBucket,
		Args:              cobra.MinimumNArgs(1),
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.DelEncryption(dst.Bucket)
		}),
	}
}
