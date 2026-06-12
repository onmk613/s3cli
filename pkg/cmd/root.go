package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"s3cli/pkg/config"
	myprint "s3cli/pkg/fmtutil"
	// "s3cli/pkg/progress"

	"github.com/spf13/cobra"
)

var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)

type cmdGroup struct {
	ID       string
	Title    string
	Commands []func() *cobra.Command
}

var (
	cmdRegistry []cmdGroup
	registryMu  sync.Mutex
)

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

func NewRootCmd() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	var (
		noColor bool
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

			myprint.SetNew(os.Stdout, noColor)

			// ── alias / completion 是元命令，不要求配置文件 ──────────
			for c := cmd; c != nil; c = c.Parent() {
				switch c.Name() {
				case "completion", "alias":
					return nil
				}
			}

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
	// pf.BoolVarP(&progress.Quiet, "quiet", "q", false, "Disable progress bar; stream plain text output instead")

	// 从注册表添加所有子命令（带分组显示）
	for _, g := range cmdRegistry {
		rootCmd.AddGroup(&cobra.Group{ID: g.ID, Title: g.Title})
		for _, fn := range g.Commands {
			cmd := fn()
			cmd.GroupID = g.ID
			rootCmd.AddCommand(cmd)
		}
	}

	err := rootCmd.Execute()
	if err != nil {
		myprint.PrintlnBoldRed(err.Error())
		os.Exit(1)
	}
}
