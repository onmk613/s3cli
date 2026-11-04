package fmtutil

// Color 是终端前景色；None 表示默认色（不着色）。
type Color int

const (
	None Color = iota
	Red
	Green
	Yellow
	Blue
	Magenta
	Cyan
	Black
	Bold
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
	Magenta:    "\033[35m",
	Cyan:       "\033[36m",
	Bold:       "\033[1m",
	BoldBlack:  "\033[1;30m",
	BoldRed:    "\033[1;31m",
	BoldGreen:  "\033[1;32m",
	BoldYellow: "\033[1;33m",
	BoldBlue:   "\033[1;34m",
	BoldCyan:   "\033[1;36m",
	Dim:        "\033[2m",
}

const resetCode = "\033[0m"

// WrapANSI 用给定 ANSI 转义序列包裹文本并在尾部补 reset 码; code 为空时
// 原样返回。供持有裸 ANSI 码的调用方 (如 progress 的配色) 复用, 与 Color
// 枚举着色共用同一处 reset 口径。
func WrapANSI(code, s string) string {
	if code == "" {
		return s
	}
	return code + s + resetCode
}
