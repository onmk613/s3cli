package cmd

import (
	"s3cli/pkg/action"
	"s3cli/pkg/utils"

	"github.com/spf13/cobra"
)

func init() { Register("config", "Bucket Configuration", NewPolicyCmd) }

func NewPolicyCmd() *cobra.Command {
	policyCmd := &cobra.Command{
		Use:   "policy",
		Short: "Manage bucket policy",
	}
	policyCmd.AddCommand(
		NewSetPolicyCmd(),
		NewGetPolicyCmd(),
		NewDelPolicyCmd(),
	)
	return policyCmd
}

func NewSetPolicyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set [policy-file] [alias:bucket] ...",
		Short: "Set bucket policy (JSON)",
		Args:  cobra.MinimumNArgs(2),
		RunE: NewRunE(ActionFunc(func(S3 action.S3Client, opts *CmdContext, s3path *utils.S3Path) error {
			return S3.SetPolicy(opts.Global.LocalFile, s3path.Bucket)
		}), &CmdContext{ArgParseMode: ParseLocalFileAndS3Path}),
	}
}

func NewGetPolicyCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "get [alias:bucket] ...",
		Aliases: []string{"ls", "list"},
		Short:   "Print current bucket policy (pretty JSON)",
		Args:    cobra.MinimumNArgs(1),
		RunE: NewRunE(ActionFunc(func(S3 action.S3Client, _ *CmdContext, s3path *utils.S3Path) error {
			return S3.GetPolicy(s3path.Bucket)
		}), &CmdContext{}),
	}
}

func NewDelPolicyCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "del [alias:bucket] ...",
		Aliases: []string{"delete", "rm", "remove"},
		Short:   "Delete bucket policy",
		Args:    cobra.MinimumNArgs(1),
		RunE: NewRunE(ActionFunc(func(S3 action.S3Client, _ *CmdContext, s3path *utils.S3Path) error {
			return S3.DelPolicy(s3path.Bucket)
		}), &CmdContext{}),
	}
}
