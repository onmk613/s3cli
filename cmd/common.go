package cmd

import (
	"context"
	"errors"
	"fmt"
	"s3cli/internal/client"
	"s3cli/internal/s3path"

	"s3cli/internal/action"
	myprint "s3cli/pkg/fmtutil"

	"github.com/spf13/cobra"
)

// errAlreadyDisplayed 是一个哨兵错误：表示错误已通过 displayError 输出给用户，
// 上层（NewRootCmd）不应再次打印，只需据此返回非零退出码。
var errAlreadyDisplayed = errors.New("error already displayed")

// isCanceled 判断错误是否由用户主动取消（Ctrl+C）引起。
func isCanceled(ctx context.Context) bool {
	return errors.Is(ctx.Err(), context.Canceled)
}

// formatUserError 将内部 error 转换为对用户友好地显示信息。
func formatUserError(err error) error {
	if err == nil {
		return nil
	}
	// 对 S3 API 错误 (s3api.ErrorResponse) 做友好格式化
	return action.FormatAPIError(err)
}

// displayError 向用户输出错误（统一入口）。
func displayError(err error) {
	myprint.PrintlnBoldRed(formatUserError(err))
}

// parseClient 封装 client 解析 + cancel + 错误展示，返回构造好的 S3Client。
func parseClient(ctx context.Context, arg string) (action.S3Client, *s3path.Path, error) {
	s3client, sp, err := client.ParsePathAndNewClient(ctx, arg)
	if err != nil {
		if errors.Is(err, s3path.ErrAliasOnly) && s3client != nil {
			return action.S3Client{S3: s3client, Alias: sp.Alias, Ctx: ctx}, sp, err
		}
		return action.S3Client{}, sp, err
	}
	return action.S3Client{S3: s3client, Alias: sp.Alias, Ctx: ctx}, sp, nil
}

// handleErr 统一处理：cancel 返回 (nil, true) 表示应静默退出；否则展示错误。
func wrapDisplayed(err error) error {
	displayError(err)
	return fmt.Errorf("%w: %w", errAlreadyDisplayed, err)
}

type ActionFunc func(S3 action.S3Client, opts *Context, s3path *s3path.Path) error

// ArgParseMode 定义 args 参数的格式
type ArgParseMode int

const (
	ParseS3OnlyPath    ArgParseMode = iota // 所有 args 都是 S3 路径
	ParseArgsAndS3Path                     // args[0] 是参数，args[1:] 是 S3 路径, 一般设置某个配置或者上传文件
	ParseS3PathAndArgs                     // args[0] 是 S3 路径，args[1] 是参数, 下载
	// ParseTwoS3Paths                             // 用于 cp/mv: args[0] 和 args[1] 都是 S3 路径
)

// Context 承载跨命令共享的"全局"选项和路径解析模式。
type Context struct {
	Global       *GlobalOptions
	ArgParseMode ArgParseMode
}

// ensureInit 保证 Global 指针非 nil。
func (c *Context) ensureInit() *Context {
	if c.Global == nil {
		c.Global = &GlobalOptions{}
	}
	return c
}

// newCmdContext 创建已初始化的 Context，可选指定 args 解析模式。
func newCmdContext(mode ...ArgParseMode) Context {
	c := Context{}
	c.ensureInit()
	if len(mode) > 0 {
		c.ArgParseMode = mode[0]
	}
	return c
}

// GlobalOptions 所有子命令通用的全局选项。
type GlobalOptions struct {
	AllowAliasOnly bool   // 是否允许仅输入 alias
	ListAll        bool   // ls --all
	Quiet          bool   //  --quiet
	Force          bool   // rb --force
	Recursive      bool   // get/put/rm/cp/mv -r
	Args           string // 某个必须在命令行中的参数, 可以指某个文件, 也可以是某个字符串
}

// NewRunE 为"单 S3 路径"命令构造 cobra RunE。
func NewRunE(fn ActionFunc, opts *Context) func(cmd *cobra.Command, args []string) error {
	if opts == nil {
		opts = &Context{}
	}
	opts.ensureInit()

	return func(cmd *cobra.Command, args []string) error {
		var s3PathArg []string

		switch opts.ArgParseMode {
		case ParseS3OnlyPath:
			s3PathArg = args
		case ParseArgsAndS3Path:
			opts.Global.Args = args[0]
			s3PathArg = args[1:]
		case ParseS3PathAndArgs:
			if len(args) >= 2 {
				opts.Global.Args = args[len(args)-1]
				s3PathArg = args[:len(args)-1]
			} else {
				s3PathArg = args
			}
		default:
			panic("NewRunE: unsupported ArgParseMode (use NewRunETwoPaths for ParseTwoS3Paths)")
		}

		var errs []error
		for _, arg := range s3PathArg {
			S3, sp, err := parseClient(cmd.Context(), arg)
			if err != nil {
				if isCanceled(cmd.Context()) {
					return nil
				}
				if !(opts.Global.AllowAliasOnly && errors.Is(err, s3path.ErrAliasOnly)) {
					displayError(err)
					errs = append(errs, err)
					continue
				}
			}
			if err := fn(S3, opts, sp); err != nil {
				if isCanceled(cmd.Context()) {
					return nil
				}
				displayError(err)
				errs = append(errs, err)
				continue
			}
		}
		if len(errs) > 0 {
			// 用 %w 同时携带哨兵与首个原始错误：errors.Is 仍能识别 errAlreadyDisplayed
			// 以抑制重复打印，errors.As 又能让 exitCodeForError 还原 404/403/取消 等退出码。
			return fmt.Errorf("%w: %w", errAlreadyDisplayed, errs[0])
		}
		return nil
	}
}

// TwoS3ActionFunc 用于需要两个 S3 路径的操作（cp/mv/diff/mirror）。
type TwoS3ActionFunc func(src, dst action.S3Client, srcPath, dstPath *s3path.Path, opts *Context) error

// NewRunETwoPaths 为双 S3 路径命令构造 RunE。
func NewRunETwoPaths(fn TwoS3ActionFunc, opts *Context) func(cmd *cobra.Command, args []string) error {
	if opts == nil {
		opts = &Context{}
	}
	opts.ensureInit()

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
		if err := fn(srcS3, dstS3, srcPath, dstPath, opts); err != nil {
			if isCanceled(cmd.Context()) {
				return nil
			}
			return wrapDisplayed(err)
		}
		return nil
	}
}
