package cmd

import (
	"context"
	"fmt"

	"s3cli/internal/action"
	"s3cli/internal/client"
	"s3cli/internal/config"
	"s3cli/internal/s3path"
	"s3cli/pkg/i18n"
	"s3cli/pkg/s3iface"

	"github.com/spf13/cobra"
)

func init() {
	Register("read", "Read Commands", NewDiffCmd)
}

// NewDiffCmd diff 命令：比较两个路径下的文件是否相同。
func NewDiffCmd() *cobra.Command {
	var (
		modeFlag string
		opt      action.DiffOptions
	)

	aliasExists := func(name string) bool {
		if config.G.S == nil {
			return false
		}
		_, ok := config.G.S[name]
		return ok
	}
	makeClient := func(ctx context.Context, sp *s3path.Path) (s3iface.S3Operations, error) {
		cli, _, err := client.ParsePathAndNewClient(formatPath(sp))
		return cli, err
	}
	parseDiffArg := func(ctx context.Context, arg string) (*action.DiffEndpoint, error) {
		return action.ParseDiffArg(ctx, arg, aliasExists, func(sp *s3path.Path) (s3iface.S3Operations, error) {
			return makeClient(ctx, sp)
		})
	}
	runDiff := func(a, b *action.DiffEndpoint) error {
		opt.A = a
		opt.B = b
		mode := action.DiffMode(modeFlag)
		switch mode {
		case action.DiffModeMD5, action.DiffModeSize, action.DiffModeQuick:
		default:
			return fmt.Errorf("invalid --check %q (use md5/size/quick)", modeFlag)
		}
		opt.Mode = mode
		err := action.Diff(opt)
		if action.IsDifferErr(err) {
			// 类似 Unix diff：有差异时以非零退出码（exitDiffer=6）告知脚本，
			// 但不再额外打印错误。走统一错误通道（而非 os.Exit），
			// 保证 root 的 defer/清理逻辑正常执行；
			// errAlreadyDisplayed 抑制重复打印。
			return fmt.Errorf("%w: %w", errAlreadyDisplayed, err)
		}
		return err
	}

	cmd := &cobra.Command{
		Use:               "diff [path-a] [path-b]",
		Short:             i18n.T("Compare files/directories between s3 and/or local paths", "比较 S3 与/或本地路径之间的文件/目录差异"),
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: AutoCompletePath,
		RunE:              NewRunEMixedPair(parseDiffArg, runDiff),
	}

	f := cmd.Flags()
	f.StringVar(&modeFlag, "check", "md5", i18n.T("Compare strategy: md5 | size | quick", "比较策略：md5 | size | quick"))
	f.BoolVarP(&opt.Recursive, "recursive", "r", true, i18n.T("Recursively diff directories", "递归比较目录"))
	f.IntVar(&opt.Concurrency, "concurrency", config.DefaultConcurrency, i18n.T("Concurrent file comparisons (directory mode)", "并发文件比较数（目录模式）"))
	f.BoolVar(&opt.BriefOnly, "brief", false, i18n.T("Print only differences, hide identical files", "只打印差异，隐藏相同的文件"))
	f.BoolVar(&opt.JSON, "json", false, jsonOutputDesc())
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
