package fmtutil

import (
	"fmt"
	"io"
	"log"
	"math"
)

var output io.Writer

func NewFormat(w io.Writer, d, c bool) {
	debug = d
	output = w
	noColor = c
}

// GetOutput 返回当前输出 writer（供 ProgressTracker 等组件使用）。
func GetOutput() io.Writer {
	return output
}

func Printf(format string, a ...any) {
	_, err := fmt.Fprintf(output, format, a...)
	if err != nil {
		log.Print(err)
	}
}
func Print(a ...any) {
	_, err := fmt.Fprint(output, a...)
	if err != nil {
		log.Print(err)
	}
}
func Println(a ...any) {
	_, err := fmt.Fprintln(output, a...)
	if err != nil {
		log.Print(err)
	}
}

func PrintfRed(format string, a ...any) {
	printfColor(output, Red, format, a...)
}
func PrintlnRed(a ...any) {
	printlnColor(output, Red, a...)
}

func PrintfGreen(format string, a ...any) {
	printfColor(output, Green, format, a...)
}
func PrintlnGreen(a ...any) {
	printlnColor(output, Green, a...)
}

func PrintfYellow(format string, a ...any) {
	printfColor(output, Yellow, format, a...)
}
func PrintlnYellow(a ...any) {
	printlnColor(output, Yellow, a...)
}

func PrintfBlue(format string, a ...any) {
	printfColor(output, Blue, format, a...)
}
func PrintlnBlue(a ...any) {
	printlnColor(output, Blue, a...)
}

func PrintfCyan(format string, a ...any) {
	printfColor(output, Cyan, format, a...)
}
func PrintlnCyan(a ...any) {
	printlnColor(output, Cyan, a...)
}

func PrintfMagenta(format string, a ...any) {
	printfColor(output, Magenta, format, a...)
}
func PrintlnMagenta(a ...any) {
	printlnColor(output, Magenta, a...)
}

func PrintfWhite(format string, a ...any) {
	printfColor(output, White, format, a...)
}
func PrintlnWhite(a ...any) {
	printlnColor(output, White, a...)
}

// Successf 成功信息（加粗绿色）
func Successf(format string, a ...any) {
	printfColor(output, BoldGreen, format, a...)
}
func Successln(a ...any) {
	printlnColor(output, BoldGreen, a...)
}

// Warnf 警告信息（加粗黄色）
func Warnf(format string, a ...any) {
	printfColor(output, BoldYellow, format, a...)
}
func Warnln(a ...any) {
	printlnColor(output, BoldYellow, a...)
}

// Errorf 错误信息（加粗红色）
func Errorf(format string, a ...any) {
	printfColor(output, BoldRed, format, a...)
}
func Errorln(a ...any) {
	printlnColor(output, BoldRed, a...)
}

// DirColor 目录颜色（蓝色）
func DirColor() Color { return Blue }

// FileColor 文件颜色（绿色）
func FileColor() Color { return Green }

// SizeColor 大小颜色（蓝色 / 青色）
func SizeColor() Color { return Cyan }

// PrintlnDim 暗淡文字
func PrintlnDim(a ...any) {
	printlnColor(output, Dim, a...)
}
func PrintfDim(format string, a ...any) {
	printfColor(output, Dim, format, a...)
}

// PrintlnBoldCyan 加粗青色
func PrintlnBoldCyan(a ...any) {
	printlnColor(output, BoldCyan, a...)
}
func PrintfBoldCyan(format string, a ...any) {
	printfColor(output, BoldCyan, format, a...)
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
