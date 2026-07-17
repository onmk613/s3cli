package fmtutil

import (
	"fmt"
	"io"
	"os"
	"sync"

	"golang.org/x/term"
)

type colorMode int

const (
	colorAuto   colorMode = iota // 自动：按 writer 是否终端
	colorAlways                  // 强制开
	colorNever                   // 强制关
)

type Printer struct {
	mu            sync.Mutex
	out           io.Writer
	mode          colorMode
	outIsTerminal bool
}

func New() *Printer {
	out := os.Stdout
	return &Printer{
		out:           out,
		mode:          colorAuto,
		outIsTerminal: isTerminal(out),
	}
}

func (p *Printer) SetColor(b bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if b {
		p.mode = colorAlways
	} else {
		p.mode = colorNever
	}
}

func (p *Printer) SetColorAuto() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.mode = colorAuto
}

func (p *Printer) SetWriter(w io.Writer) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.out = w
	p.outIsTerminal = isTerminal(w)
}

func (p *Printer) Printf(c Color, format string, args ...any) {
	p.output(c, fmt.Sprintf(format, args...))
}

func (p *Printer) Print(c Color, args ...any) {
	p.output(c, fmt.Sprint(args...))
}

func (p *Printer) Println(c Color, args ...any) {
	p.output(c, fmt.Sprintln(args...))
}

func (p *Printer) output(c Color, s string) {
	p.mu.Lock()
	noColor := p.noColorLocked()
	w := p.out
	p.mu.Unlock()
	_, _ = fmt.Fprint(w, colorize(s, c, noColor))
}

func (p *Printer) noColorLocked() bool {
	switch p.mode {
	case colorAlways:
		return false
	case colorNever:
		return true
	default:
		return !p.outIsTerminal
	}
}

func colorize(s string, c Color, noColor bool) string {
	code, ok := colorCodes[c]
	if noColor || c == None || !ok {
		return s
	}

	end := len(s)

	for end > 0 {
		if b := s[end-1]; b == '\n' || b == '\r' {
			end--
		} else {
			break
		}
	}

	// 去掉'\n'和'\r'后，字符串为空，为纯换行，则不加颜色码
	if end == 0 {
		return s
	}

	body := s[:end]
	trailing := s[end:]

	return code + body + resetCode + trailing
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}
