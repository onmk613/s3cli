package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"s3cli/internal/action"
	"s3cli/internal/config"
	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/i18n"
	"strings"
	"sync"
	"syscall"

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
		"help":             true,
		"completion":       true,
		"__complete":       true, // cobra 补全实际调用的是隐藏命令 __complete,
		"__completeNoDesc": true, // 必须一并跳过, 否则无配置文件时补全直接报错失效
		"alias":            true,
		"local-list":       true,
		"local-clear":      true,
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

// resolveLangPref 在 cobra 解析前从原始命令行/环境变量读取语言偏好：
// 优先级：--lang 标志 > CLI_LANG 环境变量 > 自动检测。
func resolveLangPref(args []string) string {
	for i, a := range args {
		if a == "--lang" && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(a, "--lang=") {
			return strings.TrimPrefix(a, "--lang=")
		}
	}
	if v := os.Getenv("CLI_LANG"); v != "" {
		return v
	}
	return "auto"
}

// NewRootCmd 创建根命令并执行，注册所有子命令
func NewRootCmd() {
	// langFlag 接收 --lang 标志值；语言已在命令构建前解析生效，
	// 该变量仅用于让标志出现在帮助中并参与环境变量绑定。
	var langFlag string

	// 语言必须在命令构建之前确定，帮助/描述文案在构造时即被渲染。
	langPref := resolveLangPref(os.Args)
	i18n.Resolve(langPref)

	// 创建一个可取消的上下文，用于在接收到 SIGINT 或 SIGTERM 信号时取消操作
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	// RootCmd
	rootCmd := &cobra.Command{
		Use:           "s3cli",
		Short:         i18n.T("A lightweight S3 command-line client", "轻量级 S3 命令行客户端"),
		Long:          i18n.T("A lightweight S3 command-line client.\n\nManage buckets and objects across multiple S3-compatible endpoints via aliases.", "轻量级 S3 命令行客户端。\n\n通过别名（alias）管理多个 S3 兼容存储端的存储桶与对象。"),
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version(),
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// 环境变量绑定对所有命令生效（含 alias/help/completion），
			// 因此放在 skip 判断之前。
			bindEnv(cmd.Flags())
			if shouldSkipConfLoad(cmd) {
				return nil
			}
			// 显式传 --no-color 才强制关色; 否则保持缺省自动模式
			// (终端且 NO_COLOR 未设置时着色, 重定向/管道自动降级纯文本)
			if config.G.F.NoColor {
				myprint.SetColor(false)
			}
			return config.LoadConf()
		},
		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			cancel()
		},
	}

	// ctx
	rootCmd.SetContext(ctx)

	// flags
	fs := rootCmd.PersistentFlags()
	fs.StringVarP(&config.G.C, "conf", "f", config.DefaultConfigPath(), i18n.T("Path to configuration file (default ~/.s3cli)", "配置文件路径（默认 ~/.s3cli）"))
	fs.BoolVar(&config.G.F.Debug, "debug", false, i18n.T("Print summarized S3 requests", "打印精简后的 S3 请求信息"))
	fs.BoolVar(&config.G.F.NoColor, "no-color", false, i18n.T("Disable color output", "禁用彩色输出"))
	fs.StringVar(&config.G.F.UserAgent, "user-agent", "", i18n.T("Override the HTTP User-Agent header", "覆盖 HTTP User-Agent 请求头"))
	fs.StringVar(&config.G.F.UserAgentSuffix, "user-agent-suffix", "", i18n.T("Append extra content to the HTTP User-Agent header", "向 HTTP User-Agent 请求头追加额外内容"))
	fs.StringArrayVarP(&config.G.F.Headers, "header", "H", nil, i18n.T("Add a custom HTTP header (key:value), can repeat", "添加自定义 HTTP 请求头（key:value），可重复使用"))
	fs.StringVar(&config.G.F.HostBase, "host-base", "", i18n.T("Override the endpoint host for all aliases", "覆盖所有别名使用的 endpoint 主机地址"))
	fs.BoolVar(&config.G.F.NoVerifySSL, "no-verify-ssl", false, i18n.T("Skip TLS certificate verification", "跳过 TLS 证书校验"))
	fs.StringVar(&langFlag, "lang", langPref, i18n.T("Help language: auto (detect from timezone/locale) | en | zh", "帮助语言：auto（按时区/环境自动检测）| en | zh"))

	// 从注册表添加所有子命令（带分组显示）。
	// 同时校验顶层命令名/别名不得重叠：cobra 在命令名与别名冲突时的命中顺序
	// 依赖注册次序、不可靠，曾导致 `rm`（删对象）被误路由到删桶命令。
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

	err := rootCmd.Execute()
	if err != nil {
		// 语义化退出码: exitCodeForError 按错误类型还原 130(取消)/4(不存在)/
		// 5(无权限)/6(diff 差异)/1(兜底)。此前恒 os.Exit(1), 定义成了死代码。
		if action.IsCanceled(err) {
			// 用户主动取消 (Ctrl+C): 不打印错误, 128+SIGINT 退出
			os.Exit(exitCanceled)
		}
		if action.IsDifferErr(err) {
			// diff 发现差异: 不是错误, 不打印, 用退出码 6 告知脚本
			os.Exit(exitDiffer)
		}
		// errAlreadyDisplayed 表示错误已在 RunE 内部输出给用户，不再重复打印。
		if !errors.Is(err, errAlreadyDisplayed) {
			myprint.PrintlnBoldRed(err.Error())
		}
		os.Exit(exitCodeForError(err))
	}
}
