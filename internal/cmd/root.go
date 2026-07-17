package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"s3cli/internal/config"
	myprint "s3cli/pkg/fmtutil"

	"github.com/spf13/cobra"
)

// Version
var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
	GoVersion = "unknown"
)

func version() string {
	return fmt.Sprintf("%s \ngolang %s \ncommit %s\nbuilt %s", Version, GoVersion, Commit, BuildDate)
}

// cmd group 用于注册顶层命令组及其子命令
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

// NewRootCmd 创建根命令并执行，注册所有子命令
func NewRootCmd() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	rootCmd := &cobra.Command{
		Use:           "s3cli",
		Short:         "A lightweight S3 command-line client (compatible with AWS S3)",
		Long:          "s3cli is a fast, dependency-free CLI for any S3-compatible object storage.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version(),
		RunE:          func(cmd *cobra.Command, args []string) error { return cmd.Help() },
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			skipCommands := map[string]bool{
				cmd.Root().Name(): true,
				"help":            true,
				"completion":      true,
				"alias":           true,
				"local-list":      true,
				"local-clear":     true,
			}
			// 不需要加载配置文件
			for c := cmd; c != nil; c = c.Parent() {
				if skipCommands[c.Name()] {
					return nil
				}
			}

			myprint.SetColor(!config.G.F.NoColor)
			return config.LoadConf()
		},
		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			cancel()
		},
	}

	rootCmd.SetContext(ctx)

	// 所有子命令通用参数
	pf := rootCmd.PersistentFlags()
	pf.StringVarP(&config.ConfPath, "conf", "f", "", "ConfPath to configuration file (default ~/.s3cli)")
	pf.BoolVar(&config.G.F.Debug, "debug", false, "Print summarized S3 requests")
	pf.BoolVar(&config.G.F.NoColor, "no-color", false, "Disable color output")
	pf.StringVar(&config.G.F.UserAgent, "user-agent", "", "Override the HTTP User-Agent header")
	pf.StringVar(&config.G.F.UserAgentSuffix, "user-agent-suffix", "", "Append extra content to the HTTP User-Agent header")
	pf.StringArrayVarP(&config.G.F.Headers, "header", "H", nil, "Add a custom HTTP header (key:value), can repeat")
	pf.BoolVarP(&config.G.F.Quiet, "quiet", "q", false, "Disable progress bar; stream plain text output instead")
	pf.BoolVar(&config.G.F.OutputJson, "json", false, "Output format: text or json (supported commands emit structured results)")

	// 从注册表添加所有子命令（带分组显示）。
	// 同时校验顶层命令名/别名不得重叠：cobra 在命令名与别名冲突时的命中顺序
	// 依赖注册次序、不可靠，曾导致 `rm`（删对象）被误路由到 `rb`（删桶）。
	// 此处 fail-fast，避免再次出现这种危险的静默路由。
	seen := make(map[string]string) // token -> 拥有它的命令 Use
	for _, g := range cmdRegistry {
		rootCmd.AddGroup(&cobra.Group{ID: g.ID, Title: g.Title})
		for _, fn := range g.Commands {
			cmd := fn()
			cmd.GroupID = g.ID
			for _, tok := range append([]string{cmd.Name()}, cmd.Aliases...) {
				if owner, dup := seen[tok]; dup {
					panic(fmt.Sprintf("top-level command token %q is claimed by both %q and %q", tok, owner, cmd.Use))
				}
				seen[tok] = cmd.Use
			}
			rootCmd.AddCommand(cmd)
		}
	}

	// 禁用help显示completion命令，保留功能，避免用户误以为是功能命令
	rootCmd.CompletionOptions.HiddenDefaultCmd = true

	err := rootCmd.Execute()
	if err != nil {
		// errAlreadyDisplayed 表示错误已在 RunE 内部输出给用户，不再重复打印。
		if !errors.Is(err, errAlreadyDisplayed) {
			myprint.PrintlnBoldRed(err.Error())
		}
		os.Exit(exitCodeForError(err))
	}
}
