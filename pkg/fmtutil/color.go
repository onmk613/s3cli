package fmtutil

var noColor bool

// quiet 全局静默标志：受 --quiet/-q 控制。
// 为 true 时，进度条等交互式 UI 应退化为流式纯文本输出。
var quiet bool

type Color int

const (
	Reset Color = iota
	Red
	Green
	Yellow
	Blue
	Cyan
	Black
	BoldBlack
	BoldGreen
	BoldYellow
	BoldRed
	BoldBlue
	BoldCyan
	Dim
)

var colorCodes = map[Color]string{
	Black:      "\033[30m",
	Red:        "\033[31m",
	Green:      "\033[32m",
	Yellow:     "\033[33m",
	Blue:       "\033[34m",
	Cyan:       "\033[36m",
	BoldBlack:  "\033[1:30m",
	BoldRed:    "\033[1;31m",
	BoldGreen:  "\033[1;32m",
	BoldYellow: "\033[1;33m",
	BoldBlue:   "\033[1;34m",
	BoldCyan:   "\033[1;36m",
	Dim:        "\033[2m",
}

const resetCode = "\033[0m"
