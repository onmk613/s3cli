package fmtutil

import (
	"fmt"
	"io"
	"log"
	"os"
	"sync"

	"golang.org/x/term"
)

// 传入一个 writer，和是否输出调试信息和是否禁用颜色用于初始化赋值
// 直接调用 Printf/Print/Println 等函数即可

var (
	output io.Writer = os.Stdout
	outMu  sync.RWMutex
)

func SetNew(w io.Writer, c bool) {
	outMu.Lock()
	defer outMu.Unlock()

	output = w
	noColor = c

	// 不是*os.File的writer，禁用颜色
	f, ok := w.(*os.File)
	if !ok {
		noColor = true
		return
	}

	// 不是终端，禁用颜色
	if !term.IsTerminal(int(f.Fd())) {
		noColor = true
	}
}

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
	defer outMu.RUnlock()
	printfColor(output, Red, format, a...)
}
func PrintlnRed(a ...any) {
	outMu.RLock()
	defer outMu.RUnlock()
	printlnColor(output, Red, a...)
}

func PrintfGreen(format string, a ...any) {
	outMu.RLock()
	defer outMu.RUnlock()
	printfColor(output, Green, format, a...)
}
func PrintlnGreen(a ...any) {
	outMu.RLock()
	defer outMu.RUnlock()
	printlnColor(output, Green, a...)
}

func PrintfYellow(format string, a ...any) {
	outMu.RLock()
	defer outMu.RUnlock()
	printfColor(output, Yellow, format, a...)
}
func PrintlnYellow(a ...any) {
	outMu.RLock()
	defer outMu.RUnlock()
	printlnColor(output, Yellow, a...)
}

func PrintfBlue(format string, a ...any) {
	outMu.RLock()
	defer outMu.RUnlock()
	printfColor(output, Blue, format, a...)
}
func PrintlnBlue(a ...any) {
	outMu.RLock()
	defer outMu.RUnlock()
	printlnColor(output, Blue, a...)
}

func PrintfCyan(format string, a ...any) {
	outMu.RLock()
	defer outMu.RUnlock()
	printfColor(output, Cyan, format, a...)
}
func PrintlnCyan(a ...any) {
	outMu.RLock()
	defer outMu.RUnlock()
	printlnColor(output, Cyan, a...)
}

func PrintfMagenta(format string, a ...any) {
	outMu.RLock()
	defer outMu.RUnlock()
	printfColor(output, Magenta, format, a...)
}
func PrintlnMagenta(a ...any) {
	outMu.RLock()
	defer outMu.RUnlock()
	printlnColor(output, Magenta, a...)
}

func PrintfWhite(format string, a ...any) {
	outMu.RLock()
	defer outMu.RUnlock()
	printfColor(output, White, format, a...)
}
func PrintlnWhite(a ...any) {
	outMu.RLock()
	defer outMu.RUnlock()
	printlnColor(output, White, a...)
}

// PrintlnDim 暗淡文字
func PrintlnDim(a ...any) {
	outMu.RLock()
	defer outMu.RUnlock()
	printlnColor(output, Dim, a...)
}
func PrintfDim(format string, a ...any) {
	outMu.RLock()
	defer outMu.RUnlock()
	printfColor(output, Dim, format, a...)
}

// PrintlnBoldCyan 加粗青色
func PrintlnBoldCyan(a ...any) {
	outMu.RLock()
	defer outMu.RUnlock()
	printlnColor(output, BoldCyan, a...)
}
func PrintfBoldCyan(format string, a ...any) {
	outMu.RLock()
	defer outMu.RUnlock()
	printfColor(output, BoldCyan, format, a...)
}

func printlnColor(w io.Writer, c Color, a ...any) {
	writeColored(w, c, fmt.Sprintln(a...))
}

func printfColor(w io.Writer, c Color, format string, a ...any) {
	writeColored(w, c, fmt.Sprintf(format, a...))
}

func writeColored(w io.Writer, c Color, s string) {
	if noColor {
		if _, err := w.Write([]byte(s)); err != nil {
			log.Print(err)
		}
		return
	}

	colored := s
	if c != Reset {
		if code, ok := colorCodes[c]; ok {
			colored = code + s + resetCode
		}
	}

	if _, err := w.Write([]byte(colored)); err != nil {
		log.Print(err)
	}
}
