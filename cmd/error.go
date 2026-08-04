package cmd

import (
	"context"
	"errors"
	"s3cli/internal/action"
	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/s3iface"
	"strings"
)

// errAlreadyDisplayed 是一个哨兵错误：表示错误已通过 displayError 输出给用户，
// 上层（NewRootCmd）不应再次打印，只需据此返回非零退出码。
var errAlreadyDisplayed = errors.New("error already displayed")

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

// isCanceled 判断错误是否由用户主动取消（Ctrl+C）引起。
func isCanceled(ctx context.Context) bool {
	return errors.Is(ctx.Err(), context.Canceled)
}

// 语义化退出码（避开 shell 约定的 0=成功 / 1=通用错误 / 2=用法错误）。
const (
	exitOK        = 0
	exitGeneric   = 1   // 兜底
	exitCanceled  = 130 // 被信号中断（128+SIGINT=130）
	exitNotFound  = 4
	exitForbidden = 5
	exitDiffer    = 6 // diff 发现差异（非错误，但需告知脚本）
)

// exitCodeForError 根据错误类型返回语义化退出码。
func exitCodeForError(err error) int {
	if err == nil {
		return exitOK
	}
	if action.IsCanceled(err) {
		return exitCanceled
	}
	if apiErr, ok := errors.AsType[*s3iface.ErrorResponse](err); ok {
		switch {
		case apiErr.StatusCode == 404 || strings.Contains(apiErr.Code, "NoSuch"):
			return exitNotFound
		case apiErr.StatusCode == 403 || strings.Contains(apiErr.Code, "AccessDenied"):
			return exitForbidden
		}
	}
	if action.IsDifferErr(err) {
		return exitDiffer
	}
	return exitGeneric
}
