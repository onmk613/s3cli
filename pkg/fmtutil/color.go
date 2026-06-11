package fmtutil

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
