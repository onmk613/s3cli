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
type Target struct {
	W     io.Writer
	Color bool
}

// multiWriter 同时向多个目标写入，并为每个目标独立决定是否着色：
type MultiWriter struct {
	Targets []Target
}

// NewMultiWriter 构造一个按目标分别着色的 writer。
func NewMultiWriter(writers ...io.Writer) io.Writer {
	mw := &MultiWriter{Targets: make([]Target, 0, len(writers))}
	for _, w := range writers {
		mw.Targets = append(mw.Targets, Target{W: w, Color: detectColor(w)})
	}
	return mw
}

// Write 写入纯文本（不含颜色）到所有目标。
func (m *MultiWriter) Write(p []byte) (int, error) {
	for _, t := range m.Targets {
		if _, err := t.W.Write(p); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

// writeColor 用指定颜色向各目标写入：着色目标写带色文本，其余写纯文本。
func (m *MultiWriter) writeColor(c Color, s string) error {
	colored := s
	if c != Reset {
		if code, ok := colorCodes[c]; ok {
			colored = code + s + resetCode
		}
	}
	for _, t := range m.Targets {
		out := s
		if t.Color {
			out = colored
		}
		if _, err := t.W.Write([]byte(out)); err != nil {
			return err
		}
	}
	return nil
}
