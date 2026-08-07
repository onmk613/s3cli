package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"s3cli/internal/action"
	"s3cli/internal/s3path"

	"github.com/spf13/cobra"
)

func init() { Register("tool", "Tools", NewShareCmd) }

// NewShareCmd 生成预签名 URL
func NewShareCmd() *cobra.Command {
	shareCmd := &cobra.Command{
		Use:   "share",
		Short: "Generate URL for temporary access to an object",
		Args:  cobra.MinimumNArgs(1),
	}
	shareCmd.AddCommand(NewShareDownloadCmd(), NewShareUploadCmd())
	addShareFlags(shareCmd, "GET")
	return shareCmd
}

func NewShareDownloadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "download [alias:bucket/path] ...",
		Aliases:           []string{"get"},
		Short:             "Generate a pre-signed URL to download (GET) an object",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: AutoCompletePath,
	}
	addShareFlags(cmd, "GET")
	return cmd
}

func NewShareUploadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "upload [alias:bucket/path] ...",
		Aliases:           []string{"put"},
		Short:             "Generate a pre-signed URL to upload (PUT) to an object",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: AutoCompletePath,
	}
	addShareFlags(cmd, "PUT")
	return cmd
}

// addShareFlags 注册 share 参数并挂载 RunE: 生成 method 对应的预签名 URL.
func addShareFlags(cmd *cobra.Command, method string) {
	var signOpt action.ShareOptions
	var expireStr string
	cmd.Flags().StringVarP(&expireStr, "expire", "E", "168h", "Expiration duration, e.g. 168h / 7d / 3600 (seconds)")
	cmd.Flags().BoolVar(&signOpt.SignV2, "v2", false, "Signature version v2")

	cmd.RunE = NewRunE(func(S3 action.Action, dst *s3path.Path) error {
		signOpt.Method = method
		if expireStr != "" {
			secs, err := parseExpireSeconds(expireStr)
			if err != nil {
				return err
			}
			signOpt.ExpireSeconds = secs
		}
		return S3.Share(signOpt, dst.Bucket, dst.Key)
	})
}

// parseExpireSeconds 解析 --expire: 支持 mc 时长串 ("168h"/"7d") 与裸秒数.
func parseExpireSeconds(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("--expire cannot be empty")
	}
	if allDigits(s) {
		n, err := strconv.Atoi(s)
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("invalid --expire %q: expected a positive number of seconds", s)
		}
		return n, nil
	}
	d, err := action.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid --expire %q: use duration like 168h/7d or seconds", s)
	}
	if d <= 0 {
		return 0, fmt.Errorf("--expire must be positive")
	}
	return int(d.Seconds()), nil
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
