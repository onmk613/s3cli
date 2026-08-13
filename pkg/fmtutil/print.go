package fmtutil

import (
	"io"
)

// 传入一个 writer，和是否输出调试信息和是否禁用颜色用于初始化赋值
// 直接调用 Printf/Print/Println 等函数即可

var std = New()

func SetColor(b bool) {
	std.SetColor(b)
}

func SetColorAuto() {
	std.SetColorAuto()
}

func SetWriter(w io.Writer) {
	std.SetWriter(w)
}

// ColorEnabled 返回全局 std 当前是否启用颜色输出。
// 依据 mode（auto/always/never）与输出目标是否为终端判断，
// 与包内 Printf/Println 等输出函数的着色决策保持一致；
// 内部加锁读取，并发安全。供其它包（如 pkg/progress）统一颜色开关。
func ColorEnabled() bool {
	std.mu.Lock()
	defer std.mu.Unlock()
	return !std.noColorLocked()
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
func PrintlnCyan(a ...any) {
	std.Println(Cyan, a...)
}

// PrintlnDim 浅灰色
func PrintlnDim(a ...any) {
	std.Println(Dim, a...)
}
func PrintfDim(format string, a ...any) {
	std.Printf(Dim, format, a...)
}

// -------- 加粗颜色输出函数 --------

// PrintlnBoldCyan 加粗青色
func PrintlnBoldCyan(a ...any) {
	std.Println(BoldCyan, a...)
}
func PrintfBoldCyan(format string, a ...any) {
	std.Printf(BoldCyan, format, a...)
}

// PrintlnBoldRed 加粗红色
func PrintlnBoldRed(a ...any) {
	std.Println(BoldRed, a...)
}
func PrintfBoldRed(format string, a ...any) {
	std.Printf(BoldRed, format, a...)
}

// PrintlnBoldGreen 加粗绿色
func PrintlnBoldGreen(a ...any) {
	std.Println(BoldGreen, a...)
}
func PrintfBoldGreen(format string, a ...any) {
	std.Printf(BoldGreen, format, a...)
}

// PrintlnBoldYellow 加粗黄色
func PrintlnBoldYellow(a ...any) {
	std.Println(BoldYellow, a...)
}
func PrintfBoldYellow(format string, a ...any) {
	std.Printf(BoldYellow, format, a...)
}

// PrintlnBoldBlue 加粗蓝色
func PrintlnBoldBlue(a ...any) {
	std.Println(BoldBlue, a...)
}
func PrintfBoldBlue(format string, a ...any) {
	std.Printf(BoldBlue, format, a...)
}

// PrintlnBoldBlack 加粗黑色
func PrintlnBoldBlack(a ...any) {
	std.Println(BoldBlack, a...)
}
func PrintfBoldBlack(format string, a ...any) {
	std.Printf(BoldBlack, format, a...)
}
