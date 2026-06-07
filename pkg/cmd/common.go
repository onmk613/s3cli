package cmd

import (
	"context"
	"errors"

	"s3cli/pkg/action"
	"s3cli/pkg/client"
	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/utils"

	"github.com/spf13/cobra"
)

// isCanceled 判断错误是否由用户主动取消（Ctrl+C）引起。
func isCanceled(ctx context.Context) bool {
	return errors.Is(ctx.Err(), context.Canceled)
}

// formatUserError 将内部 error 转换为对用户友好的显示信息。
// 优先使用 action.FormatAPIError；fallback 到 err.Error()。
func formatUserError(err error) string {
	if err == nil {
		return ""
	}
	// 对 smithy API 错误做友好格式化
	return action.FormatAPIError(err)
}

// displayError 向用户输出错误（统一入口）。
func displayError(err error) {
	myprint.Errorln(formatUserError(err))
}

type ActionFunc func(S3 action.S3Client, opts *CmdContext, s3path *utils.S3Path) error

// ArgParseMode 定义 args 参数的格式
type ArgParseMode int

const (
	ParseS3OnlyPath         ArgParseMode = iota // 所有 args 都是 S3 路径
	ParseLocalFileAndS3Path                     // args[0] 是本地文件，args[1:] 是 S3 路径
	ParseS3PathAndLocalFile                     // args[0] 是 S3 路径，args[1] 是本地文件
	ParseTwoS3Paths                             // 用于 cp/mv: args[0] 和 args[1] 都是 S3 路径
)

// CmdContext 承载跨命令共享的"全局"选项和路径解析模式。
// 各命令特有的选项由命令自身定义，不再集中存放于此。
type CmdContext struct {
	Global       *GlobalOptions
	ArgParseMode ArgParseMode
}

// ensureInit 保证 Global 指针非 nil。
func (c *CmdContext) ensureInit() *CmdContext {
	if c.Global == nil {
		c.Global = &GlobalOptions{}
	}
	return c
}

// newCmdContext 创建已初始化的 CmdContext，可选指定 args 解析模式。
func newCmdContext(mode ...ArgParseMode) CmdContext {
	c := CmdContext{}
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
	OutputJSON     bool   // info --json
	Force          bool   // rb --force
	Recursive      bool   // get/put/rm/cp/mv -r
	LocalFile      string // 本地文件路径 (get/put/cors/lifecycle/policy/event)
}

// NewRunE 为"单 S3 路径"命令构造 cobra RunE。
// fn 通过闭包捕获命令自身选项。
func NewRunE(fn ActionFunc, opts *CmdContext) func(cmd *cobra.Command, args []string) error {
	if opts == nil {
		opts = &CmdContext{}
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
			myprint.Info("processing: alias=%s bucket=%s key=%s", s3path.Alias, s3path.Bucket, s3path.Key)
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
			return errors.Join(errs...)
		}
		return nil
	}
}

// TwoS3ActionFunc 用于需要两个 S3 路径的操作（cp/mv/diff/mirror）。
type TwoS3ActionFunc func(src, dst action.S3Client, srcPath, dstPath *utils.S3Path, opts *CmdContext) error

// NewRunETwoPaths 为双 S3 路径命令构造 RunE。
func NewRunETwoPaths(fn TwoS3ActionFunc, opts *CmdContext) func(cmd *cobra.Command, args []string) error {
	if opts == nil {
		opts = &CmdContext{}
	}
	opts.ensureInit()

	return func(cmd *cobra.Command, args []string) error {
		srcClient, srcPath, err := client.ParsePathAndNewClient(cmd.Context(), args[0])
		if err != nil {
			if isCanceled(cmd.Context()) {
				return nil
			}
			displayError(err)
			return err
		}
		dstClient, dstPath, err := client.ParsePathAndNewClient(cmd.Context(), args[1])
		if err != nil {
			if isCanceled(cmd.Context()) {
				return nil
			}
			displayError(err)
			return err
		}
		srcS3 := action.S3Client{S3: srcClient, Alias: srcPath.Alias, Ctx: cmd.Context()}
		dstS3 := action.S3Client{S3: dstClient, Alias: dstPath.Alias, Ctx: cmd.Context()}
		if err := fn(srcS3, dstS3, srcPath, dstPath, opts); err != nil {
			if isCanceled(cmd.Context()) {
				return nil
			}
			displayError(err)
			return err
		}
		return nil
	}
}
