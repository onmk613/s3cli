package cmd

import (
	"context"
	"errors"
	"s3cli/internal/action"
	"s3cli/internal/api"
	myprint "s3cli/internal/fmtutil"
)

// errAlreadyDisplayed 是一个哨兵错误：表示错误已通过 displayError 输出给用户，
// 上层（NewRootCmd）不应再次打印，只需据此返回非零退出码。
var errAlreadyDisplayed = errors.New("error already displayed")

// displayError 向用户输出错误（统一入口）。
//
// 不再做二次格式化: *api.ErrorResponse 的 Error() 已是人类可读的
// "Code: Message", 上层包装也都用 %w 保留了上下文与类型链, 直接打印即可
// 既得到 "list objects: NoSuchBucket: ..." 这样完整的错误串, 又能让
// exitCodeForError 通过 errors.As 穿透到 *api.ErrorResponse。
func displayError(err error) {
	myprint.PrintlnBoldRed(err)
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
	// 判定口径统一由 api 包提供, 避免这里再写一份 404/403 嗅探。
	switch {
	case api.IsNotFound(err):
		return exitNotFound
	case api.IsAccessDenied(err):
		return exitForbidden
	}
	if action.IsDifferErr(err) {
		return exitDiffer
	}
	return exitGeneric
}
