package cmd

import (
	"context"
	"errors"
	"fmt"
	"s3cli/internal/client"
	"s3cli/internal/utils"

	"s3cli/internal/action"
	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/s3api"

	"github.com/spf13/cobra"
)

// 退出码约定：0 成功；1 通用错误；3 对象/资源不存在；4 权限拒绝；5 用户取消 / 超时。
const (
	exitGeneric  = 1
	exitNotFound = 3
	exitAccess   = 4
	exitCanceled = 5
)

// exitCodeForError 按错误类型映射退出码，供 root.go 在进程退出时使用。
//   - context.Canceled / DeadlineExceeded → 5（用户取消或超时）
//   - S3 404 (NoSuchKey/NotFound)          → 3（资源不存在）
//   - S3 403 (AccessDenied/Forbidden)      → 4（权限拒绝）
//   - 其它                                  → 1
func exitCodeForError(err error) int {
	if err == nil {
		return 0
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return exitCanceled
	}
	var apiErr *s3api.ErrorResponse
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case 404:
			return exitNotFound
		case 403:
			return exitAccess
		}
	}
	return exitGeneric
}

// errAlreadyDisplayed 是一个哨兵错误：表示错误已通过 displayError 输出给用户，
// 上层（NewRootCmd）不应再次打印，只需据此返回非零退出码。
var errAlreadyDisplayed = errors.New("error already displayed")

// isCanceled 判断错误是否由用户主动取消（Ctrl+C）引起。
func isCanceled(ctx context.Context) bool {
	return errors.Is(ctx.Err(), context.Canceled)
}

// formatUserError 将内部 error 转换为对用户友好地显示信息。
func formatUserError(err error) string {
	if err == nil {
		return ""
	}
	// 对 smithy API 错误做友好格式化
	return action.FormatAPIError(err)
}

// displayError 向用户输出错误（统一入口）。
func displayError(err error) {
	myprint.PrintlnBoldRed(formatUserError(err))
}

type ActionFunc func(S3 action.S3Client, opts *Context, s3path *utils.S3Path) error

// ArgParseMode 定义 args 参数的格式
type ArgParseMode int

const (
	ParseS3OnlyPath         ArgParseMode = iota // 所有 args 都是 S3 路径
	ParseLocalFileAndS3Path                     // args[0] 是本地文件，args[1:] 是 S3 路径
	ParseS3PathAndLocalFile                     // args[0] 是 S3 路径，args[1] 是本地文件
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
	LocalFile      string // 本地文件路径 (get/put/cors/lifecycle/policy/event)
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
		case ParseLocalFileAndS3Path:
			opts.Global.LocalFile = args[0]
			s3PathArg = args[1:]
		case ParseS3PathAndLocalFile:
			if len(args) >= 2 {
				opts.Global.LocalFile = args[len(args)-1]
				s3PathArg = args[:len(args)-1]
			} else {
				s3PathArg = args
			}
		default:
			panic("NewRunE: unsupported ArgParseMode (use NewRunETwoPaths for ParseTwoS3Paths)")
		}

		var errs []error
		for _, arg := range s3PathArg {
			s3client, s3path, err := client.ParsePathAndNewClient(cmd.Context(), arg)
			if err != nil {
				if isCanceled(cmd.Context()) {
					return nil
				}
				if !(opts.Global.AllowAliasOnly && errors.Is(err, utils.ErrAliasOnly)) {
					displayError(err)
					errs = append(errs, err)
					continue
				}
			}
			S3 := action.S3Client{S3: s3client, Alias: s3path.Alias, Ctx: cmd.Context()}
			if err := fn(S3, opts, s3path); err != nil {
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
type TwoS3ActionFunc func(src, dst action.S3Client, srcPath, dstPath *utils.S3Path, opts *Context) error

// NewRunETwoPaths 为双 S3 路径命令构造 RunE。
func NewRunETwoPaths(fn TwoS3ActionFunc, opts *Context) func(cmd *cobra.Command, args []string) error {
	if opts == nil {
		opts = &Context{}
	}
	opts.ensureInit()

	return func(cmd *cobra.Command, args []string) error {
		srcClient, srcPath, err := client.ParsePathAndNewClient(cmd.Context(), args[0])
		if err != nil {
			if isCanceled(cmd.Context()) {
				return nil
			}
			displayError(err)
			return fmt.Errorf("%w: %w", errAlreadyDisplayed, err)
		}
		dstClient, dstPath, err := client.ParsePathAndNewClient(cmd.Context(), args[1])
		if err != nil {
			if isCanceled(cmd.Context()) {
				return nil
			}
			displayError(err)
			return fmt.Errorf("%w: %w", errAlreadyDisplayed, err)
		}
		srcS3 := action.S3Client{S3: srcClient, Alias: srcPath.Alias, Ctx: cmd.Context()}
		dstS3 := action.S3Client{S3: dstClient, Alias: dstPath.Alias, Ctx: cmd.Context()}
		if err := fn(srcS3, dstS3, srcPath, dstPath, opts); err != nil {
			if isCanceled(cmd.Context()) {
				return nil
			}
			displayError(err)
			return fmt.Errorf("%w: %w", errAlreadyDisplayed, err)
		}
		return nil
	}
}
