package cmd

import (
	"context"
	client2 "s3cli/internal/client"
	"strings"
	"time"

	"s3cli/internal/action"
	"s3cli/internal/config"

	"github.com/spf13/cobra"
)

// 因为复杂的子命令嵌套，减少判断成本和补全速度, 补全函数拆分为三种
// 1. autoCompleteAlias: 补全 alias，用于 alias 相关命令补全
// 2. autoCompleteBucket: 补全alias和bucket，用于 bucket 相关命令补全
// 3. autoCompletePath: 通用的 S3 路径补全函数，支持补全 alias、bucket、key 前缀。

// test: ./s3cli __complete ls "myServer:"

// completeMaxKeys 限制单次补全返回的最大候选数
const completeMaxKeys = 50

// completeTimeout 是 shell 补全时 S3 API 调用的最大等待时间。
// 超时后静默返回空列表，避免网络异常时 tab 补全卡住。
const completeTimeout = 10 * time.Second

// AutoCompleteAlias 补全 alias，用于 alias 相关命令补全
func AutoCompleteAlias(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if config.G.S == nil {
		if err := config.LoadConf(); err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
	}

	var candidates []string
	for name := range config.G.S {
		if strings.HasPrefix(name, toComplete) {
			candidates = append(candidates, name)
		}
	}
	return candidates, cobra.ShellCompDirectiveNoFileComp
}

// AutoCompleteBucket 补全alias和bucket，用于 bucket 相关命令补全
func AutoCompleteBucket(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if config.G.S == nil {
		if err := config.LoadConf(); err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
	}

	// 还在输入 alias 部分（没有冒号）
	colon := strings.Index(toComplete, ":")
	if colon < 0 {
		return completeAliases(toComplete)
	}

	// 如果是创建 bucket命令，直接返回空列表
	if cmd.Name() == "create" {
		return nil, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
	}

	// 已包含冒号，手动做宽松解析
	alias := toComplete[:colon]
	rest := toComplete[colon+1:] // 冒号后面的部分，可能为空

	// 生成客户端
	s3Client := getClientByAlias(cmd.Context(), alias)
	if s3Client == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	// 设置超时
	ctx, cancel := context.WithTimeout(s3Client.Ctx, completeTimeout)
	defer cancel()
	s3Client.Ctx = ctx

	// 补全 bucket 名
	names, err := s3Client.CompleteBucket(rest)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

// AutoCompletePath 补全alias和bucket和key前缀，用于通用的 S3 路径补全
func AutoCompletePath(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	// 确保配置已加载（补全可能在 PersistentPreRunE 之前触发）
	if config.G.S == nil {
		if err := config.LoadConf(); err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
	}

	// 还在输入 alias 部分（没有冒号）
	colon := strings.Index(toComplete, ":")
	if colon < 0 {
		return completeAliases(toComplete)
	}
	// 已包含冒号，手动做宽松解析
	alias := toComplete[:colon]
	rest := toComplete[colon+1:] // 冒号后面的部分，可能为空

	// 生成客户端
	s3Client := getClientByAlias(cmd.Context(), alias)
	if s3Client == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	// 设置超时
	ctx, cancel := context.WithTimeout(s3Client.Ctx, completeTimeout)
	defer cancel()
	s3Client.Ctx = ctx

	// 没有 "/", 还在输入 bucket 部分
	slash := strings.Index(rest, "/")
	if slash < 0 {
		names, err := s3Client.CompleteBucket(rest)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return names, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
	}

	// 有 "/" → 正在补全 key 前缀
	bucket := rest[:slash]
	key := rest[slash+1:]
	if bucket == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	names, err := s3Client.CompleteKey(bucket, key, completeMaxKeys)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return names, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
}

// completeAliases 返回以 toComplete 为前缀的 alias 候选列表。
func completeAliases(toComplete string) ([]string, cobra.ShellCompDirective) {
	var candidates []string
	for name := range config.G.S {
		if strings.HasPrefix(name, toComplete) {
			candidates = append(candidates, name+":")
		}
	}
	return candidates, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
}

// getClientByAlias 按 alias 名获取 S3 客户端，封装为 action.S3Client
func getClientByAlias(ctx context.Context, alias string) *action.S3Client {
	static, ok := config.G.S[alias]
	if !ok {
		return nil
	}

	if cachedClient, ok := client2.S3Clients.Get(alias); ok {
		return &action.S3Client{S3: cachedClient, Alias: alias, Ctx: ctx}
	}

	s3Client, err := client2.NewS3Client(ctx, static, config.G.F)
	if err != nil {
		return nil
	}
	client2.S3Clients.Set(alias, s3Client)
	return &action.S3Client{S3: s3Client, Alias: alias, Ctx: ctx}
}

// CompleteLocalFirst 用于 args[0] 为本地路径、args[1:] 为 S3 路径的命令
// (对应 ParseArgsAndS3Path 解析模式，如 put / bucket * set)。
// 正在补全 args[0] 时委托给 shell 的默认本地文件补全；其余位置交给 s3Completer。
func CompleteLocalFirst(s3Completer cobra.CompletionFunc) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return nil, cobra.ShellCompDirectiveDefault
		}
		return s3Completer(cmd, args, toComplete)
	}
}

// CompleteLocalLast 用于最后一个 arg 为本地路径、前面为 S3 路径的命令
// (对应 ParseS3PathAndArgs 解析模式，如 get)。
// maxS3Args 为前置 S3 路径的最大数量；正在补全第 maxS3Args+1 个 arg 时
// 委托给 shell 的默认本地文件补全；其余位置交给 s3Completer。
func CompleteLocalLast(s3Completer cobra.CompletionFunc, maxS3Args int) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) >= maxS3Args {
			return nil, cobra.ShellCompDirectiveDefault
		}
		return s3Completer(cmd, args, toComplete)
	}
}
