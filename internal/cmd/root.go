package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"s3cli/internal/config"
	myprint "s3cli/pkg/fmtutil"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// 声明接收编译时注入的版本信息变量，供 version() 使用。
var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
	GoVersion = "unknown"
)

// version 返回版本信息字符串，包含 s3cli 版本、Go 版本、Git 提交哈希和构建日期。
func version() string {
	return fmt.Sprintf("%s \ngolang %s \ncommit %s\nbuilt %s", Version, GoVersion, Commit, BuildDate)
}

// cmd group 用于注册顶层命令组及其子命令
type cmdGroup struct {
	ID       string
	Title    string
	Commands []func() *cobra.Command
}

// 声明一个用于注册顶层命令组及其子命令的全局变量，供各子包在 init() 中调用 Register() 注册。
// 声明一个互斥锁，确保在多线程环境下注册命令的安全性。
var (
	cmdRegistry []cmdGroup
	registryMu  sync.Mutex
)

// Register 注册顶层命令组及其子命令。
// groupID 是命令组的唯一标识符，title 是命令组的显示标题，fn 是返回 *cobra.Command 的函数。
// 如果 groupID 已存在，则将 fn 添加到该组的 Commands 列表中；否则创建一个新的 cmdGroup 并添加到 cmdRegistry。
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

// shouldSkipConfLoad 判断命令是否跳过配置文件加载
func shouldSkipConfLoad(cmd *cobra.Command) bool {
	// 根命令自身不需要加载配置
	if cmd.Name() == cmd.Root().Name() {
		return true
	}

	// 某些命令不需要加载配置文件
	skipCommands := map[string]bool{
		"help":        true,
		"completion":  true,
		"alias":       true,
		"local-list":  true,
		"local-clear": true,
	}
	for c := cmd; c != nil; c = c.Parent() {
		if skipCommands[c.Name()] {
			return true
		}
	}
	return false
}

// bindEnv 将环境变量绑定到 cobra FlagSet
// 规则：环境变量名 = "CLI_" + flag 名称（"-" 替换为 "_"，大写）
// 优先级：命令行 > 环境变量 > 默认值
func bindEnv(fs *pflag.FlagSet) {
	fs.VisitAll(func(f *pflag.Flag) {
		// 命令行已显式设置，优先级最高，跳过
		if f.Changed {
			return
		}
		envName := "CLI_" + strings.ToUpper(strings.ReplaceAll(f.Name, "-", "_"))
		val, ok := os.LookupEnv(envName)
		if !ok || val == "" {
			return
		}
		// 用环境变量的值设置（覆盖默认值）
		if err := f.Value.Set(val); err != nil {
			fmt.Fprintf(os.Stderr, "warning: invalid value for %s: %v\n", envName, err)
		}
	})
}

// NewRootCmd 创建根命令并执行，注册所有子命令
func NewRootCmd() {
	// 创建一个可取消的上下文，用于在接收到 SIGINT 或 SIGTERM 信号时取消操作
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	// RootCmd
	rootCmd := &cobra.Command{
		Use:           "s3cli",
		Short:         "A lightweight S3 command-line client (compatible with AWS S3)",
		Long:          "s3cli is a fast, dependency-free CLI for any S3-compatible object storage.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version(),
		RunE:          func(cmd *cobra.Command, args []string) error { return cmd.Help() },
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if shouldSkipConfLoad(cmd) {
				return nil
			}
			bindEnv(cmd.Flags())
			myprint.SetColor(!config.G.F.NoColor)
			return config.LoadConf()
		},
		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			cancel()
		},
	}

	// ctx
	rootCmd.SetContext(ctx)

	// 所有子命令通用参数
	pf := rootCmd.PersistentFlags()
	pf.StringVarP(&config.ConfPath, "conf", "f", "", "Path to configuration file (default ~/.s3cli)")
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
		os.Exit(1)
	}
}
