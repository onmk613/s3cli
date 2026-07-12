package cmd

import (
	"s3cli/pkg/action"
	"s3cli/pkg/config"
	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/utils"

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
	return &cobra.Command{Use: "local-list", Aliases: []string{"ls-local"}, Short: "List local resumable multipart states", RunE: func(_ *cobra.Command, _ []string) error {
		states, err := action.ListLocalMultipartStates()
		if err != nil {
			return err
		}
		if config.G.OutputFormat == "json" {
			return myprint.PrintResult(states)
		}
		for _, state := range states {
			myprint.Printf("%s  %s/%s  uploadId=%s  %s\n", state.CreatedAt, state.Bucket, state.Key, state.UploadID, state.StatePath)
		}
		return nil
	}}
}

func newMpuLocalClearCmd() *cobra.Command {
	return &cobra.Command{Use: "local-clear [state-file]", Aliases: []string{"rm-local"}, Short: "Remove one local resumable multipart state", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		if err := action.ClearLocalMultipartState(args[0]); err != nil {
			return err
		}
		if config.G.OutputFormat == "json" {
			return myprint.PrintResult(map[string]string{"cleared": args[0]})
		}
		myprint.PrintfGreen("removed local multipart state %s\n", args[0])
		return nil
	}}
}

func newMpuListCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "list [alias:bucket[/prefix]] ...",
		Aliases:           []string{"ls"},
		Short:             "List in-progress multipart uploads",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: AutoCompletePath,
		RunE: NewRunE(func(S3 action.S3Client, _ *Context, s3path *utils.S3Path) error {
			return S3.MpuList(s3path.Bucket, s3path.Key)
		}, nil),
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
		RunE: NewRunE(func(S3 action.S3Client, _ *Context, s3path *utils.S3Path) error {
			return S3.MpuAbort(s3path.Bucket, s3path.Key, uploadID)
		}, &opts),
	}

	cmd.Flags().StringVar(&uploadID, "upload-id", "", "Specific UploadId to abort. If the object key is omitted, it is auto-resolved by listing uploads under the prefix")
	return cmd
}
