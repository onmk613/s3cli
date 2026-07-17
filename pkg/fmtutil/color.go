package fmtutil

type Color int

const (
	None Color = iota
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
	BoldBlack:  "\033[1;30m",
	BoldRed:    "\033[1;31m",
	BoldGreen:  "\033[1;32m",
	BoldYellow: "\033[1;33m",
	BoldBlue:   "\033[1;34m",
	BoldCyan:   "\033[1;36m",
	Dim:        "\033[2m",
}

const resetCode = "\033[0m"
