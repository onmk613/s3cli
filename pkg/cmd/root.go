package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"s3cli/pkg/config"
	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/s3api"

	"github.com/spf13/cobra"
)

var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
	GoVersion = "unknown"
)

func version() string {
	return fmt.Sprintf("%s \ngolang %s \ncommit %s\nbuilt %s", Version, GoVersion, Commit, BuildDate)
}

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
	var timeoutCancel context.CancelFunc

	var (
		noColor bool
	)

	rootCmd := &cobra.Command{
		Use:           "s3cli",
		Short:         "A lightweight S3 command-line client (compatible with AWS S3)",
		Long:          "s3cli is a fast, dependency-free CLI for any S3-compatible object storage.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version(),
		RunE:          func(cmd *cobra.Command, args []string) error { return cmd.Help() },
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if config.G.OutputFormat != "" && config.G.OutputFormat != "text" && config.G.OutputFormat != "json" {
				return fmt.Errorf("invalid --output %q, expected text or json", config.G.OutputFormat)
			}
			if config.G.RequestTimeout > 0 {
				if timeoutCancel != nil {
					timeoutCancel()
				}
				deadlineCtx, deadlineCancel := context.WithTimeout(cmd.Context(), config.G.RequestTimeout)
				timeoutCancel = deadlineCancel
				cmd.SetContext(deadlineCtx)
			}
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
				case "completion", "alias", "local-list", "local-clear":
					return nil
				}
			}

			if err := config.LoadConf(); err != nil {
				return err
			}
			return nil
		},
		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			if timeoutCancel != nil {
				timeoutCancel()
			}
			cancel()
		},
	}

	rootCmd.SetContext(ctx)

	pf := rootCmd.PersistentFlags()
	pf.StringVarP(&config.ConfPath, "conf", "f", "", "ConfPath to configuration file (default ~/.s3cli)")
	pf.BoolVar(&config.G.Debug, "debug", false, "Print summarized S3 requests")
	pf.BoolVar(&noColor, "no-color", false, "Disable color output")
	pf.StringVar(&config.G.UserAgent, "user-agent", "", "Override the HTTP User-Agent header")
	pf.StringVar(&config.G.UserAgentSuffix, "user-agent-suffix", "", "Append extra content to the HTTP User-Agent header")
	pf.StringArrayVarP(&config.G.Headers, "header", "H", nil, "Add a custom HTTP header (key:value), can repeat")
	pf.BoolVarP(&config.G.Quiet, "quiet", "q", false, "Disable progress bar; stream plain text output instead")
	pf.DurationVar(&config.G.RequestTimeout, "request-timeout", 0, "Maximum duration for a command's S3 requests (0 = no limit)")
	pf.StringVar(&config.G.OutputFormat, "output", "text", "Output format: text or json (supported commands emit structured results)")

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

// exitCodeForError keeps shell automation independent of localized error text.
// 1 is the generic fallback; command parsing remains Cobra's standard code 2.
func exitCodeForError(err error) int {
	if err == nil {
		return 0
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return 5
	}
	var apiErr *s3api.ErrorResponse
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case 404:
			return 3
		case 401, 403:
			return 4
		}
	}
	return 1
}
