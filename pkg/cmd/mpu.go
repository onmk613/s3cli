package cmd

import (
	"s3cli/pkg/action"
	"s3cli/pkg/utils"

	"github.com/spf13/cobra"
)

func init() { Register("object", "Object Operations", NewMpuCmd) }

func NewMpuCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mpu",
		Short: "Manage in-progress multipart uploads",
	}

	cmd.AddCommand(newMpuListCmd(), newMpuAbortCmd())
	return cmd
}

func newMpuListCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "list [alias:bucket[/prefix]] ...",
		Aliases:           []string{"ls"},
		Short:             "List in-progress multipart uploads",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: AutoCompletePath,
		RunE: NewRunE(ActionFunc(func(S3 action.S3Client, _ *CmdContext, s3path *utils.S3Path) error {
			return S3.MpuList(s3path.Bucket, s3path.Key)
		}), nil),
	}
}

func newMpuAbortCmd() *cobra.Command {
	var uploadID string
	opts := newCmdContext()
	cmd := &cobra.Command{
		Use:               "abort [alias:bucket[/key-or-prefix]]",
		Aliases:           []string{"rm", "delete", "del"},
		Short:             "Abort multipart upload(s). With --upload-id aborts one; otherwise aborts all under the prefix.",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: AutoCompletePath,
		RunE: NewRunE(ActionFunc(func(S3 action.S3Client, _ *CmdContext, s3path *utils.S3Path) error {
			return S3.MpuAbort(s3path.Bucket, s3path.Key, uploadID)
		}), &opts),
	}

	cmd.Flags().StringVar(&uploadID, "upload-id", "", "Specific UploadId to abort. If the object key is omitted, it is auto-resolved by listing uploads under the prefix")
	return cmd
}
