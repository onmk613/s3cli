package cmd

import (
	"fmt"

	"s3cli/internal/client"
	"s3cli/internal/s3path"

	"s3cli/internal/action"
	"s3cli/internal/config"
	"s3cli/pkg/s3api"

	"github.com/spf13/cobra"
)

func init() {
	Register("read", "Read Commands", NewDiffCmd)
}

// NewDiffCmd diff 命令：比较两个路径下的文件是否相同。
func NewDiffCmd() *cobra.Command {
	var (
		modeFlag    string
		recursive   bool
		concurrency int
		briefOnly   bool
	)

	cmd := &cobra.Command{
		Use:               "diff [path-a] [path-b]",
		Short:             "Compare files/directories between s3 and/or local paths",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: AutoCompletePath,
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := action.DiffMode(modeFlag)
			switch mode {
			case action.DiffModeMD5, action.DiffModeSize, action.DiffModeQuick:
			default:
				return fmt.Errorf("invalid --check %q (use md5/size/quick)", modeFlag)
			}

			aliasExists := func(name string) bool {
				if config.G.S == nil {
					return false
				}
				_, ok := config.G.S[name]
				return ok
			}
			makeClient := func(sp *s3path.Path) (*s3api.Client, error) {
				cli, _, err := client.ParsePathAndNewClient(cmd.Context(), formatPath(sp))
				return cli, err
			}

			a, err := action.ParseDiffArg(cmd.Context(), args[0], aliasExists, makeClient)
			if err != nil {
				if isCanceled(cmd.Context()) {
					return nil
				}
				return fmt.Errorf("parse %q: %w", args[0], err)
			}
			b, err := action.ParseDiffArg(cmd.Context(), args[1], aliasExists, makeClient)
			if err != nil {
				if isCanceled(cmd.Context()) {
					return nil
				}
				return fmt.Errorf("parse %q: %w", args[1], err)
			}

			err = action.Diff(action.DiffOptions{
				A:           a,
				B:           b,
				Mode:        mode,
				Recursive:   recursive,
				Concurrency: concurrency,
				BriefOnly:   briefOnly,
			})
			// 用户主动取消（Ctrl+C）：静默退出，不打印错误。
			if isCanceled(cmd.Context()) || action.IsCanceled(err) {
				return nil
			}
			if action.IsDifferErr(err) {
				// 类似 Unix diff：有差异时退出码为 1，但不再额外打印错误。
				// 走统一错误通道（而非 os.Exit），保证 root 的 defer/清理逻辑正常执行;
				// errAlreadyDisplayed 抑制重复打印, exitCodeForError 归一到退出码 1。
				return fmt.Errorf("%w: %w", errAlreadyDisplayed, err)
			}
			return err
		},
	}

	f := cmd.Flags()
	f.StringVar(&modeFlag, "check", "md5", "Compare strategy: md5 | size | quick")
	f.BoolVarP(&recursive, "recursive", "r", true, "Recursively diff directories")
	f.IntVar(&concurrency, "concurrency", config.DefaultConcurrency, "Concurrent file comparisons (directory mode)")
	f.BoolVar(&briefOnly, "brief", false, "Print only differences, hide identical files")
	return cmd
}

// formatPath 把已解析的 S3Path 还原成 "alias:bucket/key" 字符串。
// 仅用于复用 ParsePathAndNewClient 的客户端缓存逻辑。
// 调用方 (ParseDiffArg) 已保证 sp.Bucket 非空。
func formatPath(sp *s3path.Path) string {
	key := sp.Key
	if sp.TrailingSlash && key != "" && key[len(key)-1] != '/' {
		key += "/"
	}
	if key == "" {
		return sp.Alias + ":" + sp.Bucket
	}
	return sp.Alias + ":" + sp.Bucket + "/" + key
}
