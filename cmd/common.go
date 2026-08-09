package cmd

import (
	"context"
	"errors"
	"fmt"
	"s3cli/internal/action"
	"s3cli/internal/client"
	"s3cli/internal/s3path"

	"github.com/spf13/cobra"
)

var AllowAliasOnly bool

const (
	AnnoArgParseMode = "ArgParseMode"
	LocalFileOrPath  = "LocalFileOrPath"

	OnlyS3Path           = "OnlyS3Path"
	FirstLocalFileOrPath = "FirstLocalFileOrPath"
	LastLocalFileOrPath  = "LastLocalFileOrPath"
)

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
			return nil, nil, fmt.Errorf("%s: 至少需要 2 个参数, 实际 %d", cmd.CommandPath(), len(args))
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
		return nil, nil, fmt.Errorf("%s: 不支持的 %s=%q（双 s3 路径请用 NewRunETwoPaths）",
			cmd.CommandPath(), AnnoArgParseMode, mode)
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

// ── 唯一的实现 ────────────────────────────────────────
func NewRunEWithMode(fn ActionFuncWithMode) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		s3Args, opts, err := splitArgs(cmd, args)
		if err != nil {
			return err // 用法错误，未打印过，交给上层统一输出 + usage
		}

		var errs []error
		for _, arg := range s3Args {
			if isCanceled(ctx) {
				return nil
			}

			S3, sp, err := parseClient(ctx, arg)
			if err != nil && !(AllowAliasOnly && errors.Is(err, s3path.ErrAliasOnly)) {
				displayError(err)
				errs = append(errs, err)
				continue
			}

			if err := fn(S3, sp, opts); err != nil {
				if isCanceled(ctx) {
					return nil
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
	return NewRunEWithMode(func(S3 action.Action, sp *s3path.Path, _ ArgParseMode) error {
		return fn(S3, sp)
	})
}

// NewRunELocal 供不解析 S3 路径的本地命令使用，统一 cancel 与错误展示语义。
func NewRunELocal(fn func(cmd *cobra.Command, args []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if err := fn(cmd, args); err != nil {
			if isCanceled(cmd.Context()) {
				return nil
			}
			return wrapDisplayed(err)
		}
		return nil
	}
}

// NewRunEMixedPair 供两个参数各自可能是本地路径或 S3 路径的命令（diff）使用，
// 统一参数解析、cancel 与错误包装。
func NewRunEMixedPair[T any](parse func(ctx context.Context, arg string) (T, error), run func(a, b T) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		if len(args) != 2 {
			return fmt.Errorf("expected 2 arguments, got %d", len(args))
		}
		a, err := parse(ctx, args[0])
		if err != nil {
			if isCanceled(ctx) {
				return nil
			}
			return fmt.Errorf("parse %q: %w", args[0], err)
		}
		b, err := parse(ctx, args[1])
		if err != nil {
			if isCanceled(ctx) {
				return nil
			}
			return fmt.Errorf("parse %q: %w", args[1], err)
		}
		if err := run(a, b); err != nil {
			if isCanceled(ctx) || action.IsCanceled(err) {
				return nil
			}
			return err
		}
		return nil
	}
}

// NewRunETwoPaths 为双 S3 路径命令构造 RunE。
func NewRunETwoPaths(fn TwoS3ActionFunc) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		srcS3, srcPath, err := parseClient(cmd.Context(), args[0])
		if err != nil {
			if isCanceled(cmd.Context()) {
				return nil
			}
			return wrapDisplayed(err)
		}
		dstS3, dstPath, err := parseClient(cmd.Context(), args[1])
		if err != nil {
			if isCanceled(cmd.Context()) {
				return nil
			}
			return wrapDisplayed(err)
		}
		if err := fn(srcS3, dstS3, srcPath, dstPath); err != nil {
			if isCanceled(cmd.Context()) {
				return nil
			}
			return wrapDisplayed(err)
		}
		return nil
	}
}

// parseClient 封装 client 解析 + cancel + 错误展示，返回构造好的 S3Client。
func parseClient(ctx context.Context, arg string) (action.Action, *s3path.Path, error) {
	s3client, sp, err := client.ParsePathAndNewClient(arg)
	if err != nil {
		if errors.Is(err, s3path.ErrAliasOnly) && s3client != nil {
			return action.Action{S3: s3client, Alias: sp.Alias, Ctx: ctx}, sp, err
		}
		return action.Action{}, sp, err
	}
	return action.Action{S3: s3client, Alias: sp.Alias, Ctx: ctx}, sp, nil
}

// handleErr 统一处理：cancel 返回 (nil, true) 表示应静默退出；否则展示错误。
func wrapDisplayed(err error) error {
	displayError(err)
	return fmt.Errorf("%w: %w", errAlreadyDisplayed, err)
}
