package cmd

import (
	"s3cli/pkg/action"
	"s3cli/pkg/utils"

	"github.com/spf13/cobra"
)

func init() { Register("config", "Bucket Configuration", NewEncryptionCmd) }

func NewEncryptionCmd() *cobra.Command {
	encryptionCmd := &cobra.Command{
		Use:   "encryption",
		Short: "Manage bucket(s) default encryption (SSE-S3 / SSE-KMS)",
	}
	encryptionCmd.AddCommand(
		NewSetEncryptionCmd(),
		NewGetEncryptionCmd(),
		NewDelEncryptionCmd(),
	)
	return encryptionCmd
}

func NewSetEncryptionCmd() *cobra.Command {
	var encOpt action.EncryptionOptions
	opts := newCmdContext()
	cmd := &cobra.Command{
		Use:   "set [alias:bucket] ...",
		Short: "Set bucket(s) default encryption (SSE-S3 / SSE-KMS)",
		Args:  cobra.MinimumNArgs(1),
		RunE: NewRunE(ActionFunc(func(S3 action.S3Client, _ *CmdContext, s3path *utils.S3Path) error {
			return S3.SetEncryption(encOpt, s3path.Bucket)
		}), &opts),
	}

	cmd.Flags().StringVar(&encOpt.Algorithm, "algorithm", "AES256", "Encryption algorithm: AES256 / aws:kms")
	cmd.Flags().StringVar(&encOpt.KMSKeyID, "kms-key-id", "", "KMS key id (required for aws:kms)")
	cmd.Flags().BoolVar(&encOpt.BucketKey, "bucket-key", false, "Enable S3 Bucket Key (aws:kms only)")
	cmd.Flags().StringVar(&encOpt.ConfigFile, "from-file", "", "Load AWS CLI JSON config instead of using flags")

	return cmd
}

func NewGetEncryptionCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "get [alias:bucket] ...",
		Aliases: []string{"ls", "list"},
		Short:   "Print bucket(s) default encryption configuration (JSON)",
		Args:    cobra.MinimumNArgs(1),
		RunE: NewRunE(ActionFunc(func(S3 action.S3Client, _ *CmdContext, s3path *utils.S3Path) error {
			return S3.GetEncryption(s3path.Bucket)
		}), nil),
	}
}

func NewDelEncryptionCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "del [alias:bucket] ...",
		Aliases: []string{"delete", "rm", "remove"},
		Short:   "Delete bucket(s) default encryption configuration",
		Args:    cobra.MinimumNArgs(1),
		RunE: NewRunE(ActionFunc(func(S3 action.S3Client, _ *CmdContext, s3path *utils.S3Path) error {
			return S3.DelEncryption(s3path.Bucket)
		}), nil),
	}
}
