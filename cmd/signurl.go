package cmd

import (
	"s3cli/internal/action"
	"s3cli/internal/s3path"

	"github.com/spf13/cobra"
)

func init() { Register("tool", "Tools", NewShareCmd) }

func NewShareCmd() *cobra.Command {
	var signOpt action.ShareOptions
	opts := newCmdContext()
	cmd := &cobra.Command{
		Use:               "share [alias:bucket/path] ...",
		Short:             "Print pre-signed S3 URLs",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: AutoCompletePath,
		RunE: NewRunE(func(S3 action.S3Client, _ *Context, s3path *s3path.Path) error {
			return S3.Share(signOpt, s3path.Bucket, s3path.Key)
		}, &opts),
	}

	cmd.Flags().IntVarP(&signOpt.ExpireSeconds, "expire", "e", 604800, "Expiration time in seconds (default 7 days)")
	cmd.Flags().StringVarP(&signOpt.Method, "method", "m", "GET", "HTTP method: GET / PUT / DELETE / HEAD")
	cmd.Flags().BoolVar(&signOpt.SignV2, "v2", false, "Signature version v2")
	return cmd
}
