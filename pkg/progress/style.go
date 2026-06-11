package progress

import "unicode"

// Style 定义进度条的填充物与颜色
type Style struct {
	// 边框
	LeftBracket  string // 左边框，如 "["，留空则无
	RightBracket string // 右边框，如 "]"，留空则无
	// 填充物
	Filled string // 已完成部分的填充字符，如 "=" "█" "🚀"
	Head   string // 当前进度位置的字符（绘制在已完成与未完成之间），如 ">"，留空则不绘制
	Empty  string // 未完成部分的填充字符，如 " " "░"

	// 着色
	BarColor string
}

const ansiReset = "\033[0m"

// StyleShade 深浅阴影风格：▓▓▓▓█░░░░
func DefaultStyle() Style {
	return Style{
		LeftBracket:  "",
		RightBracket: "",
		Filled:       "▓",
		Head:         "█",
		Empty:        "░",
	}
}

// normalize 为缺省字段填入合理默认值，保证渲染不出错。
func (s Style) normalize() Style {
	if s.Filled == "" {
		s.Filled = "="
	}
	if s.Empty == "" {
		s.Empty = " "
	}
	return s
}

// displayWidth 估算字符串在终端中的显示列宽。
// CJK 全角字符与大多数 emoji 占 2 列，其余占 1 列。组合字符（如变体选择符、
// 零宽连接符）不占宽度。该实现无外部依赖，覆盖常见场景。
func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		w += runeWidth(r)
	}
	return w
}

func runeWidth(r rune) int {
	switch {
	case r == 0:
		return 0
	case r < 32 || (r >= 0x7f && r < 0xa0):
		// 控制字符
		return 0
	case unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r):
		// 组合标记 / 格式字符（含零宽连接符、变体选择符）
		return 0
	case r == 0x200d || (r >= 0xfe00 && r <= 0xfe0f):
		return 0
	case isWide(r):
		return 2
	default:
		return 1
	}
}

// isWide 判断 rune 是否为东亚全角或宽 emoji。
func isWide(r rune) bool {
	return (r >= 0x1100 && r <= 0x115f) || // Hangul Jamo
		(r >= 0x2e80 && r <= 0x303e) || // CJK 部首等
		(r >= 0x3041 && r <= 0x33ff) || // 平假名/片假名/CJK 符号
		(r >= 0x3400 && r <= 0x4dbf) || // CJK 扩展 A
		(r >= 0x4e00 && r <= 0x9fff) || // CJK 统一表意
		(r >= 0xa000 && r <= 0xa4cf) || // 彝文
		(r >= 0xac00 && r <= 0xd7a3) || // 谚文音节
		(r >= 0xf900 && r <= 0xfaff) || // CJK 兼容表意
		(r >= 0xfe30 && r <= 0xfe4f) || // CJK 兼容形式
		(r >= 0xff00 && r <= 0xff60) || // 全角形式
		(r >= 0xffe0 && r <= 0xffe6) ||
		(r >= 0x1f300 && r <= 0x1faff) || // emoji & 符号
		(r >= 0x20000 && r <= 0x3fffd) // CJK 扩展 B+
}
