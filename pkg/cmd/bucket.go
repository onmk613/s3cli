package cmd

import (
	"s3cli/pkg/action"
	"s3cli/pkg/utils"

	"github.com/spf13/cobra"
)

func init() {
	Register("bucket", "Bucket Management", NewMbCmd)
	Register("bucket", "Bucket Management", NewRbCmd)
}

func NewMbCmd() *cobra.Command {
	var mkOpt action.MakeBucketOptions
	opts := newCmdContext()
	cmd := &cobra.Command{
		Use:     "mb [alias:bucket] ...",
		Aliases: []string{"create", "make"},
		Short:   "Create new bucket(s)",
		Args:    cobra.MinimumNArgs(1),
		RunE: NewRunE(ActionFunc(func(S3 action.S3Client, _ *CmdContext, s3path *utils.S3Path) error {
			return S3.MakeBuckets(mkOpt, s3path.Bucket)
		}), &opts),
	}
	cmd.Flags().StringVar(&mkOpt.CorsFile, "set-cors", "", "cors-file")
	cmd.Flags().StringVar(&mkOpt.LifecycleFile, "set-lifecycle", "", "lifecycle-file")
	cmd.Flags().StringVar(&mkOpt.PolicyFile, "set-policy", "", "policy-file")
	return cmd
}

func NewRbCmd() *cobra.Command {
	opts := newCmdContext()
	cmd := &cobra.Command{
		Use:     "rb [alias:bucket] ...",
		Aliases: []string{"remove-bucket"},
		Short:   "Remove bucket(s)",
		Args:    cobra.MinimumNArgs(1),
		RunE: NewRunE(ActionFunc(func(S3 action.S3Client, opts *CmdContext, s3path *utils.S3Path) error {
			return S3.RemoveBuckets(s3path.Bucket, opts.Global.Force)
		}), &opts),
	}
	cmd.Flags().BoolVar(&opts.Global.Force, "force", false, "Force remove bucket even if not empty")
	return cmd
}
