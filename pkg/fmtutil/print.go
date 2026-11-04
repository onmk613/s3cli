package fmtutil

import (
	"io"
)

// 包级便捷函数分两个通道:
//   - Print*/Println* 委托全局 std (os.Stdout, 自动着色), 只输出命令结果;
//   - Err* 委托 errOut (os.Stderr, 自动着色), 输出错误/警告/交互提示。
// SetColor/SetColorAuto 同时作用于两个通道, --no-color 不会只关一半。

func SetColor(b bool) {
	std.SetColor(b)
	errOut.SetColor(b)
}

func SetColorAuto() {
	std.SetColorAuto()
	errOut.SetColorAuto()
}

func SetWriter(w io.Writer) {
	std.SetWriter(w)
}

// SetErrWriter 切换诊断输出目标（测试用）；生产代码使用 os.Stderr。
func SetErrWriter(w io.Writer) {
	errOut.SetWriter(w)
}

// ColorEnabled 返回全局 std 当前是否启用颜色输出，与包级输出函数的
// 着色决策保持一致，供其它包（如 pkg/progress）统一颜色开关。
func ColorEnabled() bool {
	return std.ColorEnabled()
}

// ErrColorEnabled 返回诊断 Printer 当前是否启用颜色输出。
func ErrColorEnabled() bool {
	return errOut.ColorEnabled()
}

// ------- 默认(无色)输出函数 -------

func Printf(format string, a ...any) {
	std.Printf(None, format, a...)
}
func Print(a ...any) {
	std.Print(None, a...)
}
func Println(a ...any) {
	std.Println(None, a...)
}

// ------- 颜色函数 -------

// PrintfRed 红色
func PrintfRed(format string, a ...any) {
	std.Printf(Red, format, a...)
}
func PrintlnRed(a ...any) {
	std.Println(Red, a...)
}

// PrintfGreen 绿色
func PrintfGreen(format string, a ...any) {
	std.Printf(Green, format, a...)
}
func PrintlnGreen(a ...any) {
	std.Println(Green, a...)
}

// PrintfYellow 黄色
func PrintfYellow(format string, a ...any) {
	std.Printf(Yellow, format, a...)
}
func PrintlnYellow(a ...any) {
	std.Println(Yellow, a...)
}

// PrintfBlue 蓝色
func PrintfBlue(format string, a ...any) {
	std.Printf(Blue, format, a...)
}
func PrintlnBlue(a ...any) {
	std.Println(Blue, a...)
}

// PrintfCyan 青色
func PrintfCyan(format string, a ...any) {
	std.Printf(Cyan, format, a...)
}

// PrintfDim 浅灰色
func PrintfDim(format string, a ...any) {
	std.Printf(Dim, format, a...)
}

// -------- 加粗颜色输出函数 --------

// PrintfBoldCyan 加粗青色
func PrintfBoldCyan(format string, a ...any) {
	std.Printf(BoldCyan, format, a...)
}

// PrintlnBoldRed 加粗红色
func PrintlnBoldRed(a ...any) {
	std.Println(BoldRed, a...)
}

// PrintfBoldGreen 加粗绿色
func PrintfBoldGreen(format string, a ...any) {
	std.Printf(BoldGreen, format, a...)
}

// PrintfBoldYellow 加粗黄色
func PrintfBoldYellow(format string, a ...any) {
	std.Printf(BoldYellow, format, a...)
}

// PrintfBoldBlue 加粗蓝色
func PrintfBoldBlue(format string, a ...any) {
	std.Printf(BoldBlue, format, a...)
}

// ------- 诊断输出函数 (stderr) -------
//
// 这些函数与上面的 stdout 版本一一对应, 但写入 os.Stderr。错误、警告、
// 交互提示等“不构成命令结果”的内容必须走这里, 保证 stdout 只包含数据。

func ErrPrint(a ...any) {
	errOut.Print(None, a...)
}
func ErrPrintf(format string, a ...any) {
	errOut.Printf(None, format, a...)
}
func ErrPrintln(a ...any) {
	errOut.Println(None, a...)
}
func ErrPrintfRed(format string, a ...any) {
	errOut.Printf(Red, format, a...)
}
func ErrPrintlnRed(a ...any) {
	errOut.Println(Red, a...)
}
func ErrPrintfYellow(format string, a ...any) {
	errOut.Printf(Yellow, format, a...)
}
func ErrPrintlnYellow(a ...any) {
	errOut.Println(Yellow, a...)
}
func ErrPrintfBlue(format string, a ...any) {
	errOut.Printf(Blue, format, a...)
}
func ErrPrintlnBlue(a ...any) {
	errOut.Println(Blue, a...)
}
func ErrPrintfGreen(format string, a ...any) {
	errOut.Printf(Green, format, a...)
}
func ErrPrintfBoldRed(format string, a ...any) {
	errOut.Printf(BoldRed, format, a...)
}
func ErrPrintlnBoldRed(a ...any) {
	errOut.Println(BoldRed, a...)
}
func ErrPrintfBoldYellow(format string, a ...any) {
	errOut.Printf(BoldYellow, format, a...)
}
func ErrPrintfBoldBlue(format string, a ...any) {
	errOut.Printf(BoldBlue, format, a...)
}
