package progress

import (
	"fmt"
	"strings"
	"time"

	"s3cli/pkg/utils"
)

// buildBar 构建进度条字符串
// buildBar 构建进度条字符串（前后留出边距，防止塞满终端）
func (pt *Tracker) buildBar(wd int) string {
	t := pt.total.Load()
	d := pt.done.Load()
	tsz := pt.totalSz.Load()
	dsz := pt.doneSz.Load()
	startAt := time.Since(pt.startAt)

	// 1. 计算速率
	rate := "0 B/s"
	if dsz > 0 && startAt.Seconds() > 0.5 {
		rate = utils.FormatBytes(int64(float64(dsz)/startAt.Seconds())) + "/s"
	}

	// 2. 计算 ETA 与耗时
	var eta string
	elapsedStr := formatDuration(startAt)
	// 条件：已传输数据 > 0 且 运行超过 0.1 秒（防止除以 0）
	if dsz > 0 && tsz > dsz && startAt.Seconds() > 0.1 {
		// 1. 计算总平均速率 (Byte/s)
		avgRate := float64(dsz) / startAt.Seconds()

		// 2. 剩余字节数 / 平均速率 = 剩余秒数
		if avgRate > 0 {
			remainingBytes := float64(tsz - dsz)
			etaSec := remainingBytes / avgRate

			// 3. 转换为 Duration 并格式化
			etaDuration := time.Duration(etaSec * float64(time.Second))
			etaStr := formatDuration(etaDuration)

			eta = fmt.Sprintf("%s | %s", elapsedStr, etaStr)
		}
	} else {
		eta = elapsedStr
	}

	// 3. 计算百分比 (%3d%% 保持 3 位右对齐，避免 9% -> 10% 时整个进度条左右抖动)
	var pct float64
	if tsz > 0 {
		pct = float64(dsz) * 100 / float64(tsz)
	}
	if pct > 100 {
		pct = 100
	}

	// 4. 构建右侧文本
	countStr := fmt.Sprintf("%d/%d", d, t)
	sizeStr := fmt.Sprintf("%s/%s | %s | %3d%%", utils.FormatBytes(dsz), utils.FormatBytes(tsz), rate, int(pct))

	var rightStr string
	if t > 0 && tsz > 0 {
		rightStr = fmt.Sprintf("%s | %s | %s", countStr, sizeStr, eta)
	} else if t > 0 && tsz == 0 {
		rightStr = countStr
	} else if t == 0 && tsz > 0 {
		rightStr = fmt.Sprintf("%s | %s", sizeStr, eta)
	}

	// 5. 定义左右边距与内部元素间距
	// marginWidth = 左边留1格 + 右边留1格 = 2 列
	const marginWidth = 2

	st := pt.style
	labelWidth := stringWidth(pt.label)
	rightWidth := stringWidth(rightStr)
	bracketWidth := stringWidth(st.LeftBracket) + stringWidth(st.RightBracket)

	// 内部空格计算：label后1空格 + bar与rightStr间1空格
	spacingWidth := 0
	if labelWidth > 0 {
		spacingWidth++
	}
	if rightWidth > 0 {
		spacingWidth++
	}

	// 留给进度条主体 [████░░░] 的列宽 = 终端总宽 - 左右外边距 - 组件宽度 - 内部间距
	barArea := wd - marginWidth - labelWidth - rightWidth - spacingWidth - bracketWidth

	// 6. 宽度极窄时的安全降级
	if barArea < 5 {
		if labelWidth > 0 && wd >= labelWidth+rightWidth+marginWidth+1 {
			return colorize(pt.color.Stats, fmt.Sprintf(" %s %s ", pt.label, rightStr))
		}
		return colorize(pt.color.Stats, fmt.Sprintf(" %s ", rightStr))
	}

	// 7. 生成进度条主体
	bar := buildStyledBar(st, barArea, pct/100)

	// 8. 组合输出（首尾显式拼接 " "，留出视觉呼吸感）
	var sb strings.Builder
	sb.WriteString(" ") // 1. 左侧留空 1 格

	if labelWidth > 0 {
		sb.WriteString(pt.label)
		sb.WriteString(" ")
	}
	sb.WriteString(bar)
	if rightWidth > 0 {
		sb.WriteString(" ")
		sb.WriteString(rightStr)
	}

	sb.WriteString(" ") // 2. 右侧留空 1 格

	return colorize(pt.color.Stats, sb.String())
}

// buildStyledBar 按给定样式绘制进度条主体（含边框与着色）。
// barArea 为进度条内部可用的显示列宽，frac 为完成比例 [0,1]。
func buildStyledBar(st *Style, barArea int, frac float64) string {
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}

	// Filled/Head/Empty 单元均按 1 显示列计（▓/█/░/=/# 等）。
	hasHead := st.Head != ""

	// 以列宽为单位计算已完成列数
	filledCols := int(frac*float64(barArea) + 0.5)
	if filledCols > barArea {
		filledCols = barArea
	}

	// 预留进度头的 1 列：未满且有 Head 时，头占 1 列
	useHead := hasHead && filledCols < barArea && filledCols >= 1
	if useHead {
		filledCols--
	}
	emptyCols := barArea - filledCols
	if useHead {
		emptyCols--
	}
	if emptyCols < 0 {
		emptyCols = 0
	}

	var b strings.Builder
	b.WriteString(st.LeftBracket)
	b.WriteString(repeatToWidth(st.Filled, filledCols))
	if useHead {
		b.WriteString(st.Head)
	}
	b.WriteString(repeatToWidth(st.Empty, emptyCols))
	b.WriteString(st.RightBracket)
	return b.String()
}

// repeatToWidth 用占 1 显示列的 unit 字符填满 cols 个显示列
// unit 为空时用空格填充, 每个 unit 占 1 列，因此重复 cols 次即可
func repeatToWidth(unit string, cols int) string {
	if cols <= 0 {
		return ""
	}
	if unit == "" {
		return strings.Repeat(" ", cols)
	}
	var b strings.Builder
	b.Grow(cols * len(unit))
	for i := 0; i < cols; i++ {
		b.WriteString(unit)
	}
	return b.String()
}

// colorize 用给定 ANSI 颜色包裹文本；noColor 时原样返回
func colorize(color, s string) string {
	if color == "" {
		return s
	}
	return color + s + ansiReset
}

// formatDuration 将 time.Duration 格式化为易读字符串
// 示例：1h2m3s / 2m3s / 3s
func formatDuration(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	d = d.Round(time.Second)

	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	if hours > 0 {
		return fmt.Sprintf("%dh%02dm%02ds", hours, minutes, seconds)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm%02ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}

// stringWidth 计算字符串在终端中的实际显示列宽（去除 ANSI 序列并处理多字节字符）
func stringWidth(s string) int {
	// 如果字符串包含 ANSI 颜色代码，需要先去除 ANSI 代码再计算宽度
	cleanStr := stripANSI(s)
	// 如果含有中文等 East Asian 字符，可以使用 runewidth.StringWidth(cleanStr)
	// 若全是 ASCII 字母/数字/符号，直接用 len(cleanStr) 即可
	return len(cleanStr)
}

// stripANSI 简单的 ANSI 转义序列剥离函数
func stripANSI(str string) string {
	var b strings.Builder
	inSequence := false
	for _, r := range str {
		if r == '\x1b' {
			inSequence = true
			continue
		}
		if inSequence {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inSequence = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
