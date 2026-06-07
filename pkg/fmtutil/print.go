// Package fmtutil 提供格式化输出、彩色打印、进度条和日志功能。
package fmtutil

import (
	"fmt"
	"io"
	"log"
	"math"
	"sync"
)

var (
	output io.Writer
	outMu  sync.RWMutex // 保护 output / debug / noColor 并发访问
)

func NewFormat(w io.Writer, d, c bool) {
	outMu.Lock()
	defer outMu.Unlock()
	debug = d
	output = w
	noColor = c
}

// GetOutput 返回当前输出 writer（供 ProgressTracker 等组件使用）。
func GetOutput() io.Writer {
	outMu.RLock()
	defer outMu.RUnlock()
	return output
}

func Printf(format string, a ...any) {
	outMu.RLock()
	defer outMu.RUnlock()
	_, err := fmt.Fprintf(output, format, a...)
	if err != nil {
		log.Print(err)
	}
}
func Print(a ...any) {
	outMu.RLock()
	defer outMu.RUnlock()
	_, err := fmt.Fprint(output, a...)
	if err != nil {
		log.Print(err)
	}
}
func Println(a ...any) {
	outMu.RLock()
	defer outMu.RUnlock()
	_, err := fmt.Fprintln(output, a...)
	if err != nil {
		log.Print(err)
	}
}

func PrintfRed(format string, a ...any) {
	outMu.RLock()
	printfColor(output, Red, format, a...)
	outMu.RUnlock()
}
func PrintlnRed(a ...any) {
	outMu.RLock()
	printlnColor(output, Red, a...)
	outMu.RUnlock()
}

func PrintfGreen(format string, a ...any) {
	outMu.RLock()
	printfColor(output, Green, format, a...)
	outMu.RUnlock()
}
func PrintlnGreen(a ...any) {
	outMu.RLock()
	printlnColor(output, Green, a...)
	outMu.RUnlock()
}

func PrintfYellow(format string, a ...any) {
	outMu.RLock()
	printfColor(output, Yellow, format, a...)
	outMu.RUnlock()
}
func PrintlnYellow(a ...any) {
	outMu.RLock()
	printlnColor(output, Yellow, a...)
	outMu.RUnlock()
}

func PrintfBlue(format string, a ...any) {
	outMu.RLock()
	printfColor(output, Blue, format, a...)
	outMu.RUnlock()
}
func PrintlnBlue(a ...any) {
	outMu.RLock()
	printlnColor(output, Blue, a...)
	outMu.RUnlock()
}

func PrintfCyan(format string, a ...any) {
	outMu.RLock()
	printfColor(output, Cyan, format, a...)
	outMu.RUnlock()
}
func PrintlnCyan(a ...any) {
	outMu.RLock()
	printlnColor(output, Cyan, a...)
	outMu.RUnlock()
}

func PrintfMagenta(format string, a ...any) {
	outMu.RLock()
	printfColor(output, Magenta, format, a...)
	outMu.RUnlock()
}
func PrintlnMagenta(a ...any) {
	outMu.RLock()
	printlnColor(output, Magenta, a...)
	outMu.RUnlock()
}

func PrintfWhite(format string, a ...any) {
	outMu.RLock()
	printfColor(output, White, format, a...)
	outMu.RUnlock()
}
func PrintlnWhite(a ...any) {
	outMu.RLock()
	printlnColor(output, White, a...)
	outMu.RUnlock()
}

// Successf 成功信息（加粗绿色）
func Successf(format string, a ...any) {
	outMu.RLock()
	printfColor(output, BoldGreen, format, a...)
	outMu.RUnlock()
}
func Successln(a ...any) {
	outMu.RLock()
	printlnColor(output, BoldGreen, a...)
	outMu.RUnlock()
}

// Warnf 警告信息（加粗黄色）
func Warnf(format string, a ...any) {
	outMu.RLock()
	printfColor(output, BoldYellow, format, a...)
	outMu.RUnlock()
}
func Warnln(a ...any) {
	outMu.RLock()
	printlnColor(output, BoldYellow, a...)
	outMu.RUnlock()
}

// Errorf 错误信息（加粗红色）
func Errorf(format string, a ...any) {
	outMu.RLock()
	printfColor(output, BoldRed, format, a...)
	outMu.RUnlock()
}
func Errorln(a ...any) {
	outMu.RLock()
	printlnColor(output, BoldRed, a...)
	outMu.RUnlock()
}

// DirColor 目录颜色（蓝色）
func DirColor() Color { return Blue }

// FileColor 文件颜色（绿色）
func FileColor() Color { return Green }

// SizeColor 大小颜色（蓝色 / 青色）
func SizeColor() Color { return Cyan }

// PrintlnDim 暗淡文字
func PrintlnDim(a ...any) {
	outMu.RLock()
	printlnColor(output, Dim, a...)
	outMu.RUnlock()
}
func PrintfDim(format string, a ...any) {
	outMu.RLock()
	printfColor(output, Dim, format, a...)
	outMu.RUnlock()
}

// PrintlnBoldCyan 加粗青色
func PrintlnBoldCyan(a ...any) {
	outMu.RLock()
	printlnColor(output, BoldCyan, a...)
	outMu.RUnlock()
}
func PrintfBoldCyan(format string, a ...any) {
	outMu.RLock()
	printfColor(output, BoldCyan, format, a...)
	outMu.RUnlock()
}

func printlnColor(w io.Writer, c Color, a ...any) {
	writeColored(w, c, fmt.Sprintln(a...))
}

func printfColor(w io.Writer, c Color, format string, a ...any) {
	writeColored(w, c, fmt.Sprintf(format, a...))
}

// FormatBytes 格式化字节数为人类可读。
func FormatBytes(bytes int64) string {
	if bytes <= 0 {
		return "0 B"
	}
	units := []string{"B", "KB", "MB", "GB", "TB", "PB", "EB"}
	base := 1024.0
	exp := int(math.Log(float64(bytes)) / math.Log(base))
	if exp >= len(units) {
		exp = len(units) - 1
	}
	value := float64(bytes) / math.Pow(base, float64(exp))
	return fmt.Sprintf("%.2f %s", value, units[exp])
}

// writeColored 把字符串按颜色写入 w。
// 若 w 是 multiWriter，则交给它对每个目标分别决定是否着色；
// 否则按 detectColor 的结果对整段输出统一着色。
func writeColored(w io.Writer, c Color, s string) {
	if mw, ok := w.(*multiWriter); ok {
		if err := mw.writeColor(c, s); err != nil {
			log.Fatal(err)
		}
		return
	}
	if detectColor(w) && c != Reset {
		if code, ok := colorCodes[c]; ok {
			s = code + s + resetCode
		}
	}
	if _, err := w.Write([]byte(s)); err != nil {
		log.Fatal(err)
	}
}
