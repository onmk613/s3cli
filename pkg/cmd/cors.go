package cmd

import (
	"s3cli/pkg/action"
	"s3cli/pkg/utils"

	"github.com/spf13/cobra"
)

func init() {
	Register("config", "Bucket Configuration", NewCorsCmd)
}

func NewCorsCmd() *cobra.Command {
	corsCmd := &cobra.Command{
		Use:   "cors",
		Short: "Manage CORS configuration for buckets",
	}
	corsCmd.AddCommand(
		NewSetCorsCmd(),
		NewGetCorsCmd(),
		NewDelCorsCmd(),
	)
	return corsCmd
}

func NewSetCorsCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "set [cors-file] [alias:bucket] ...",
		Aliases: []string{"s"},
		Short:   "Set CORS rules for bucket(s) (xml or json, AWS CLI compatible)",
		Args:    cobra.MinimumNArgs(2),
		RunE: NewRunE(ActionFunc(func(S3 action.S3Client, opts *CmdContext, s3path *utils.S3Path) error {
			return S3.SetCors(opts.Global.LocalFile, s3path.Bucket)
		}), &CmdContext{ArgParseMode: ParseLocalFileAndS3Path}),
	}
}

func NewGetCorsCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "get [alias:bucket] ...",
		Aliases: []string{"ls", "list", "l"},
		Short:   "Print CORS rules of bucket(s) as JSON",
		Args:    cobra.MinimumNArgs(1),
		RunE: NewRunE(ActionFunc(func(S3 action.S3Client, _ *CmdContext, s3path *utils.S3Path) error {
			return S3.GetCors(s3path.Bucket)
		}), nil),
	}
}

func NewDelCorsCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "del [alias:bucket] ...",
		Aliases: []string{"delete", "rm", "remove", "d"},
		Short:   "Delete CORS rules for bucket(s)",
		Args:    cobra.MinimumNArgs(1),
		RunE: NewRunE(ActionFunc(func(S3 action.S3Client, _ *CmdContext, s3path *utils.S3Path) error {
			return S3.DelCors(s3path.Bucket)
		}), nil),
	}
}
