// Package cmd 定义 s3cli 的所有命令与子命令，基于 cobra 框架。
// 提供命令自注册机制、统一的 RunE 工厂、全局选项管理。
package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"s3cli/pkg/config"
	myprint "s3cli/pkg/fmtutil"

	"github.com/spf13/cobra"
)

// ScrollMax 进度条滚动刷屏条数（0=全部显示）
var ScrollMax = 5
var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)

// ── 命令自注册机制 ────────────────────────────────────────────────
// 新增命令只需在对应文件中调用 cmd.Register，无需修改 root.go。
// groupID 用于 cobra --help 分组显示，title 为分组标题。

type cmdGroup struct {
	ID       string
	Title    string
	Commands []func() *cobra.Command
}

var (
	cmdRegistry []cmdGroup
	registryMu  sync.Mutex
)

// Register 向指定分组注册一个命令工厂。
// groupID 对应 cobra.Group.ID，title 为 --help 中显示的分组标题。
// 应在各命令文件的 init() 中调用。
func Register(groupID, title string, fn func() *cobra.Command) {
	registryMu.Lock()
	defer registryMu.Unlock()
	for i := range cmdRegistry {
		if cmdRegistry[i].ID == groupID {
			cmdRegistry[i].Commands = append(cmdRegistry[i].Commands, fn)
			return
		}
	}
	cmdRegistry = append(cmdRegistry, cmdGroup{ID: groupID, Title: title, Commands: []func() *cobra.Command{fn}})
}

// NewRootCmd 构造 s3cmd 根命令。
func NewRootCmd() *cobra.Command {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	var (
		noColor bool
		toFile  string
		timeout time.Duration
	)

	rootCmd := &cobra.Command{
		Use:           "s3cli",
		Short:         "A lightweight S3 command-line client (compatible with AWS S3)",
		Long:          "s3cli is a fast, dependency-free CLI for any S3-compatible object storage.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       fmt.Sprintf("%s (commit %s, built %s)", Version, Commit, BuildDate),
		RunE:          func(cmd *cobra.Command, args []string) error { return cmd.Help() },
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			switch cmd.Name() {
			case "help", "version":
				return nil
			}

			if cmd.Name() == cmd.Root().Name() {
				return nil
			}

			// 若指定了 --timeout，为当前上下文添加超时
			if timeout > 0 {
				ctxWithTimeout, cancelTimeout := context.WithTimeout(ctx, timeout)
				cmd.SetContext(ctxWithTimeout)
				origCancel := cancel
				cancel = func() {
					cancelTimeout()
					origCancel()
				}
			}

			// ── 初始化输出（所有命令都需要，含 alias/completion）────────
			if toFile != "" {
				logFile, err := myprint.OpenLogFile(toFile)
				if err != nil {
					myprint.Printf("failed to open log file: %v", err)
					myprint.NewFormat(os.Stdout, config.G.Debug, noColor)
				} else {
					w := myprint.NewMultiWriter(os.Stdout, logFile)
					myprint.NewFormat(w, config.G.Debug, noColor)
				}
			} else {
				myprint.NewFormat(os.Stdout, config.G.Debug, noColor)
			}

			// ── alias / completion 是元命令，不要求配置文件 ──────────
			for c := cmd; c != nil; c = c.Parent() {
				switch c.Name() {
				case "completion", "alias":
					return nil
				}
			}

			// 运行时日志：记录执行命令和参数
			myprint.Info("command: %s, args: %v", cmd.CommandPath(), args)

			if err := config.LoadConf(); err != nil {
				return err
			}
			return nil
		},
		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			cancel()
		},
	}

	rootCmd.SetContext(ctx)

	pf := rootCmd.PersistentFlags()
	pf.StringVarP(&config.ConfigPath, "conf", "f", "", "Path to configuration file (default ~/.s3cli)")
	pf.BoolVar(&config.G.Debug, "debug", false, "Print summarized S3 requests")
	pf.BoolVar(&noColor, "no-color", false, "Disable color output")
	pf.StringVar(&toFile, "logfile", "", "Write output to file instead of stdout")
	pf.IntVar(&ScrollMax, "scroll", 5, "Progress scroll lines (0=show all, default 5)")
	pf.DurationVar(&timeout, "timeout", 0, "Maximum execution time (e.g. 30s, 5m). 0 means no limit.")

	// 从注册表添加所有子命令（带分组显示）
	for _, g := range cmdRegistry {
		rootCmd.AddGroup(&cobra.Group{ID: g.ID, Title: g.Title})
		for _, fn := range g.Commands {
			cmd := fn()
			cmd.GroupID = g.ID
			rootCmd.AddCommand(cmd)
		}
	}
	return rootCmd
}
