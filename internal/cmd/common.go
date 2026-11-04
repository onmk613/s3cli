package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/onmk613/s3cli/internal/action"
	"github.com/onmk613/s3cli/internal/client"
	"github.com/onmk613/s3cli/internal/s3path"
	"github.com/onmk613/s3cli/pkg/i18n"
	"github.com/spf13/cobra"
)

const (
	AnnoArgParseMode = "ArgParseMode"
	LocalFileOrPath  = "LocalFileOrPath"

	OnlyS3Path           = "OnlyS3Path"
	FirstLocalFileOrPath = "FirstLocalFileOrPath"
	LastLocalFileOrPath  = "LastLocalFileOrPath"
)

// 常见 flag 的共享双语描述，集中定义以保证多个命令间的文案一致、单一维护点。
// 以函数形式返回 T() 结果：语言在命令构建前已解析，此处求值时机安全。

func vidAliasDesc() string {
	return i18n.T("Alias of --version-id", "--version-id 的别名")
}

func scAliasDesc() string {
	return i18n.T("Alias of --storage-class", "--storage-class 的别名")
}

func quietDesc() string {
	return i18n.T("Disable progress bar; stream plain text output instead", "禁用进度条；改为流式纯文本输出")
}

// The First one
// The last one

type ArgParseMode map[string]string

var (
	OnlyS3PathMode           = ArgParseMode{AnnoArgParseMode: OnlyS3Path}
	FirstLocalFileOrPathMode = ArgParseMode{AnnoArgParseMode: FirstLocalFileOrPath}
	LastLocalFileOrPathMode  = ArgParseMode{AnnoArgParseMode: LastLocalFileOrPath}
)

// ActionFunc 默认, args[] 中只有s3path, ls/cat等等
type ActionFunc func(S3 action.Action, dst *s3path.Path) error

// ActionFuncWithMode 有一个args不是s3path, put/get等等需要传入本地路径, 不做s3Path解析
type ActionFuncWithMode func(S3 action.Action, dst *s3path.Path, opts ArgParseMode) error

// TwoS3ActionFunc 用于需要两个 S3 路径的操作（cp/mv/mirror）
type TwoS3ActionFunc func(src, dst action.Action, srcPath, dstPath *s3path.Path) error

// splitArgs 按 annotation 把 args 切成「s3 路径列表」+「附加参数」
func splitArgs(cmd *cobra.Command, args []string) ([]string, ArgParseMode, error) {
	opts := ArgParseMode{}

	switch mode := cmd.Annotations[AnnoArgParseMode]; mode {
	case "", OnlyS3Path:
		return args, opts, nil

	case FirstLocalFileOrPath: // 首参非 s3 路径，如 put localfile s3://...
		if len(args) < 2 {
			return nil, nil, fmt.Errorf(i18n.T("%s: at least 2 arguments required, got %d", "%s：至少需要 2 个参数，实际 %d"), cmd.CommandPath(), len(args))
		}
		opts[LocalFileOrPath] = args[0]
		return args[1:], opts, nil

	case LastLocalFileOrPath: // 末参非 s3 路径，如 get s3://... localdir
		if len(args) < 2 {
			return args, opts, nil // 单参数时视为纯 s3 路径（沿用原语义）
		}
		opts[LocalFileOrPath] = args[len(args)-1]
		return args[:len(args)-1], opts, nil

	default:
		return nil, nil, fmt.Errorf(i18n.T("%s: unsupported argument parse mode %s=%q",
			"%s：不支持的参数解析模式 %s=%q"), cmd.CommandPath(), AnnoArgParseMode, mode)
	}
}

// ── 统一收尾 ──────────────────────────────────────────
// 双重 %w：errors.Is 识别 errAlreadyDisplayed 抑制重复打印，
// errors.As 穿透到原始错误以便 exitCodeForError 还原语义化退出码。
func wrapErrs(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %w", errAlreadyDisplayed, errs[0])
}

// newRunEWithMode 是 RunE 工厂的核心实现。
// allowAliasOnly 决定是否容忍「仅别名」参数 (s3path.ErrAliasOnly)：
// 只有需要列出所有桶的 ls 需要；该值在命令构建时即被闭包捕获，
// 避免曾经用包级变量 AllowAliasOnly 造成的跨命令泄漏。
func newRunEWithMode(allowAliasOnly bool, fn ActionFuncWithMode) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		// 客户端缓存只在本次命令执行内有效: 多次解析同一 alias (如多参数
		// 命令) 复用连接池, 命令结束即释放, 不进入包级全局状态。
		resolver := client.NewResolver()

		s3Args, opts, err := splitArgs(cmd, args)
		if err != nil {
			return err // 用法错误，未打印过，交给上层统一输出（SilenceUsage 下不再附带 usage）
		}

		var errs []error
		for _, arg := range s3Args {
			if isCanceled(ctx) {
				// 用户取消: 返回 ctx 错误, 由 root 统一映射为退出码 130 (不打印)
				return ctx.Err()
			}

			S3, sp, err := parseClient(ctx, resolver, arg)
			if err != nil && (!allowAliasOnly || !errors.Is(err, s3path.ErrAliasOnly)) {
				// 仅别名对 ls 合法, 但对其余命令是"缺少 bucket"。这里补一条
				// 面向用户的明确提示, 而不是把内部的 "alias only" 哨兵文案
				// 直接抛给用户。
				if errors.Is(err, s3path.ErrAliasOnly) && sp != nil {
					err = fmt.Errorf(i18n.T(
						"invalid s3 path %q: bucket is required after %q",
						"无效的 S3 路径 %q：别名 %q 后必须指定存储桶"), arg, sp.Alias)
				}
				displayError(err)
				errs = append(errs, err)
				continue
			}

			if err := fn(S3, sp, opts); err != nil {
				if isCanceled(ctx) {
					return ctx.Err()
				}
				displayError(err)
				errs = append(errs, err)
				continue
			}
		}
		return wrapErrs(errs)
	}
}

// NewRunE 用于「args 全是 s3 路径」的命令（ls / cat / rm …）
func NewRunE(fn ActionFunc) func(*cobra.Command, []string) error {
	return newRunEWithMode(false, func(S3 action.Action, sp *s3path.Path, _ ArgParseMode) error {
		return fn(S3, sp)
	})
}

// NewRunEWithMode 用于有一个参数不是 s3 路径的命令 (put/get), 不做 s3Path 解析。
func NewRunEWithMode(fn ActionFuncWithMode) func(*cobra.Command, []string) error {
	return newRunEWithMode(false, fn)
}

// NewRunEAllowAliasOnly 用于允许「仅别名」参数的命令 (ls: 列出所有桶)。
// 与 NewRunE 的唯一区别是容忍 s3path.ErrAliasOnly, 且只作用于当前命令。
func NewRunEAllowAliasOnly(fn ActionFunc) func(*cobra.Command, []string) error {
	return newRunEWithMode(true, func(S3 action.Action, sp *s3path.Path, _ ArgParseMode) error {
		return fn(S3, sp)
	})
}

// NewRunELocal 供不解析 S3 路径的本地命令使用，统一 cancel 与错误展示语义。
func NewRunELocal(fn func(cmd *cobra.Command, args []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if err := fn(cmd, args); err != nil {
			if isCanceled(cmd.Context()) {
				return cmd.Context().Err()
			}
			return wrapDisplayed(err)
		}
		return nil
	}
}

// NewRunEMixedPair 供两个参数各自可能是本地路径或 S3 路径的命令（diff）使用，
// 统一参数解析、cancel 与错误包装。
// parse 拿到本次命令执行共享的 client.Resolver, 两个端点命中同一 alias 时
// 可复用客户端与连接池。
// run 返回的错误（含 diff 差异哨兵 errDiffer）原样上抛, 由 exitCodeForError
// 映射为 130 (取消) / 6 (差异) 等语义化退出码。
func NewRunEMixedPair[T any](parse func(ctx context.Context, r *client.Resolver, arg string) (T, error), run func(a, b T) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		if len(args) != 2 {
			return fmt.Errorf("expected 2 arguments, got %d", len(args))
		}
		resolver := client.NewResolver()
		a, err := parse(ctx, resolver, args[0])
		if err != nil {
			if isCanceled(ctx) {
				return ctx.Err()
			}
			return fmt.Errorf("parse %q: %w", args[0], err)
		}
		b, err := parse(ctx, resolver, args[1])
		if err != nil {
			if isCanceled(ctx) {
				return ctx.Err()
			}
			return fmt.Errorf("parse %q: %w", args[1], err)
		}
		if err := run(a, b); err != nil {
			if isCanceled(ctx) {
				return ctx.Err()
			}
			return err
		}
		return nil
	}
}

// NewRunETwoPaths 为双 S3 路径命令构造 RunE。
func NewRunETwoPaths(fn TwoS3ActionFunc) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		// 源与目标同属一个 alias 时 (常见: cp/mirror 同端点), 共享解析器
		// 可以复用同一个 http.Transport, 省掉一次 TCP/TLS 握手。
		resolver := client.NewResolver()
		srcS3, srcPath, err := parseClient(cmd.Context(), resolver, args[0])
		if err != nil {
			if isCanceled(cmd.Context()) {
				return cmd.Context().Err()
			}
			return wrapDisplayed(err)
		}
		dstS3, dstPath, err := parseClient(cmd.Context(), resolver, args[1])
		if err != nil {
			if isCanceled(cmd.Context()) {
				return cmd.Context().Err()
			}
			return wrapDisplayed(err)
		}
		if err := fn(srcS3, dstS3, srcPath, dstPath); err != nil {
			if isCanceled(cmd.Context()) {
				return cmd.Context().Err()
			}
			return wrapDisplayed(err)
		}
		return nil
	}
}

// parseClient 封装 client 解析 + cancel + 错误展示，返回构造好的 S3Client。
// resolver 由调用方按「一次命令执行」创建, 保证缓存不跨命令泄漏。
func parseClient(ctx context.Context, resolver *client.Resolver, arg string) (action.Action, *s3path.Path, error) {
	s3client, sp, err := resolver.ParsePath(arg)
	if err != nil {
		if errors.Is(err, s3path.ErrAliasOnly) && s3client != nil {
			return action.Action{S3: s3client, Alias: sp.Alias, Ctx: ctx}, sp, err
		}
		return action.Action{}, sp, err
	}
	return action.Action{S3: s3client, Alias: sp.Alias, Ctx: ctx}, sp, nil
}

// wrapDisplayed 展示错误并包装 errAlreadyDisplayed 哨兵, 抑制上层重复打印。
func wrapDisplayed(err error) error {
	displayError(err)
	return fmt.Errorf("%w: %w", errAlreadyDisplayed, err)
}
