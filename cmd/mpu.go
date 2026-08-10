package cmd

import (
	"s3cli/internal/action"
	"s3cli/internal/s3path"

	"github.com/spf13/cobra"
)

func init() { Register("object", "Object Operations", NewMpuCmd) }

func NewMpuCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mpu",
		Short: "Manage in-progress multipart uploads",
	}

	cmd.AddCommand(newMpuListCmd(), newMpuAbortCmd(), newMpuLocalListCmd(), newMpuLocalClearCmd())
	return cmd
}

func newMpuLocalListCmd() *cobra.Command {
	var opt action.MpuLocalOptions
	cmd := &cobra.Command{
		Use:     "local-list",
		Aliases: []string{"ls-local"},
		Short:   "List local resumable multipart states",
		RunE: NewRunELocal(func(_ *cobra.Command, _ []string) error {
			return action.MpuLocalList(opt)
		}),
	}
	cmd.Flags().BoolVar(&opt.OutputToJSON, "json", false, "Output format: text or json (supported commands emit structured results)")
	return cmd
}

func newMpuLocalClearCmd() *cobra.Command {
	var opt action.MpuLocalOptions
	cmd := &cobra.Command{
		Use:     "local-clear [state-file]",
		Aliases: []string{"rm-local"},
		Short:   "Remove one local resumable multipart state",
		Args:    cobra.ExactArgs(1),
		RunE: NewRunELocal(func(_ *cobra.Command, args []string) error {
			return action.MpuLocalClear(args[0], opt)
		}),
	}
	cmd.Flags().BoolVar(&opt.OutputToJSON, "json", false, "Output format: text or json (supported commands emit structured results)")
	return cmd
}

func newMpuListCmd() *cobra.Command {
	var opt action.MpuListOptions
	cmd := &cobra.Command{
		Use:               "list [alias:bucket[/prefix]] ...",
		Aliases:           []string{"ls"},
		Short:             "List in-progress multipart uploads",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: AutoCompletePath,
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.MpuList(opt, dst.Bucket, dst.Key)
		}),
	}
	cmd.Flags().BoolVar(&opt.JSON, "json", false, "Output format: text or json (supported commands emit structured results)")
	return cmd
}

func newMpuAbortCmd() *cobra.Command {
	var uploadID string
	cmd := &cobra.Command{
		Use:               "abort [alias:bucket[/key-or-prefix]]",
		Aliases:           []string{"rm", "delete", "del"},
		Short:             "Abort multipart upload. With --upload-id aborts one; otherwise aborts all under the prefix.",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: AutoCompletePath,
		RunE: NewRunE(func(S3 action.Action, dst *s3path.Path) error {
			return S3.MpuAbort(dst.Bucket, dst.Key, uploadID)
		}),
	}

	cmd.Flags().StringVar(&uploadID, "upload-id", "", "Specific UploadId to abort. If the object key is omitted, it is auto-resolved by listing uploads under the prefix")
	return cmd
}
