package cmd

import (
	"s3cli/pkg/action"
	"s3cli/pkg/utils"

	"github.com/spf13/cobra"
)

func init() { Register("tool", "Tools", NewSignUrlCmd) }

func NewSignUrlCmd() *cobra.Command {
	var signOpt action.SignurlOptions
	opts := newCmdContext()
	cmd := &cobra.Command{
		Use:               "signurl [alias:bucket/path] ...",
		Short:             "Print pre-signed S3 URLs",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: AutoCompletePath,
		RunE: NewRunE(ActionFunc(func(S3 action.S3Client, _ *CmdContext, s3path *utils.S3Path) error {
			return S3.Signurl(signOpt, s3path.Bucket, s3path.Key)
		}), &opts),
	}

	cmd.Flags().IntVarP(&signOpt.ExpireSeconds, "expire", "e", 604800, "Expiration time in seconds (default 7 days)")
	cmd.Flags().StringVarP(&signOpt.Method, "method", "m", "GET", "HTTP method: GET / PUT / DELETE / HEAD")
	cmd.Flags().BoolVar(&signOpt.SignurlV2, "v2", false, "Signature version v2")
	return cmd
}
