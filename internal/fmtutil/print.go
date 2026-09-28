package fmtutil

import (
	"io"
)

// 包级便捷函数均委托全局 std（os.Stdout、自动着色）：
// 通过 SetColor/SetWriter 调整全局行为，直接调用
// Printf/Println 等函数即可输出。

func SetColor(b bool) {
	std.SetColor(b)
}

func SetColorAuto() {
	std.SetColorAuto()
}

func SetWriter(w io.Writer) {
	std.SetWriter(w)
}

// ColorEnabled 返回全局 std 当前是否启用颜色输出，与包级输出函数的
// 着色决策保持一致，供其它包（如 pkg/progress）统一颜色开关。
func ColorEnabled() bool {
	return std.ColorEnabled()
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
