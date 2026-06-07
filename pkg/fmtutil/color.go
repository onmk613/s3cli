package fmtutil

import (
	"io"
	"os"

	"golang.org/x/term"
)

var noColor bool

type Color int

const (
	Reset Color = iota
	Red
	Green
	Yellow
	Blue
	Cyan
	Magenta
	White
	Black
	BoldGreen
	BoldYellow
	BoldRed
	BoldBlue
	BoldCyan
	Dim
)

var colorCodes = map[Color]string{
	Reset:      "\033[0m",
	Red:        "\033[31m",
	Green:      "\033[32m",
	Yellow:     "\033[33m",
	Blue:       "\033[34m",
	Cyan:       "\033[36m",
	Magenta:    "\033[35m",
	White:      "\033[37m",
	Black:      "\033[30m",
	BoldGreen:  "\033[1;32m",
	BoldYellow: "\033[1;33m",
	BoldRed:    "\033[1;31m",
	BoldBlue:   "\033[1;34m",
	BoldCyan:   "\033[1;36m",
	Dim:        "\033[2m",
}

const resetCode = "\033[0m"

func detectColor(w io.Writer) bool {
	if noColor {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// target 表示一个输出目标及其独立的着色策略。
type target struct {
	w     io.Writer
	color bool
}

// multiWriter 同时向多个目标写入，并为每个目标独立决定是否着色：
// 终端目标写带 ANSI 颜色的文本，文件等非终端目标写纯文本。
type multiWriter struct {
	targets []target
}

// NewMultiWriter 构造一个按目标分别着色的 writer。
// 每个传入的目标会在构造时根据 noColor 与是否为终端来决定其着色策略。
func NewMultiWriter(writers ...io.Writer) io.Writer {
	mw := &multiWriter{targets: make([]target, 0, len(writers))}
	for _, w := range writers {
		mw.targets = append(mw.targets, target{w: w, color: detectColor(w)})
	}
	return mw
}

// Write 写入纯文本（不含颜色）到所有目标。
// 带颜色的写入由 writeColor 处理，从而对每个目标分别决定颜色。
func (m *multiWriter) Write(p []byte) (int, error) {
	for _, t := range m.targets {
		if _, err := t.w.Write(p); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

// writeColor 用指定颜色向各目标写入：着色目标写带色文本，其余写纯文本。
func (m *multiWriter) writeColor(c Color, s string) error {
	colored := s
	if c != Reset {
		if code, ok := colorCodes[c]; ok {
			colored = code + s + resetCode
		}
	}
	for _, t := range m.targets {
		out := s
		if t.color {
			out = colored
		}
		if _, err := t.w.Write([]byte(out)); err != nil {
			return err
		}
	}
	return nil
}
