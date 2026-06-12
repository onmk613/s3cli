package progress

import (
	"fmt"
	"strings"
	"time"

	"s3cli/pkg/utils"
)

// buildBar 构建进度条字符串
func (pt *ProgressTracker) buildBar(wd int) string {
	t := pt.total.Load()
	d := pt.done.Load()

	tsz := pt.totalSz.Load()
	dsz := pt.doneSz.Load()

	elapsed := time.Since(pt.startAt)

	// 计算速率
	rate := "0 B/s"
	if dsz > 0 && elapsed.Seconds() > 0.5 {
		rate = utils.FormatBytes(int64(float64(dsz)/elapsed.Seconds())) + "/s"
	}
	// 计算预计剩余时间 (ETA)
	var eta string
	if dsz > 0 && tsz > dsz && elapsed.Seconds() > 0.5 {
		// 根据当前速率估算剩余时间
		etaSec := elapsed.Seconds() * float64(tsz-dsz) / float64(dsz)
		if etaSec < 3600 {
			eta = fmt.Sprintf("ETA:%02d:%02d", int(etaSec)/60, int(etaSec)%60)
		} else {
			h := int(etaSec) / 3600
			m := (int(etaSec) % 3600) / 60
			eta = fmt.Sprintf("ETA:%dh%02dm", h, m)
		}
	} else {
		eta = "ETA:--"
	}

	// 计算百分比
	var pct float64
	if tsz > 0 {
		pct = float64(dsz) * 100 / float64(tsz)
	}
	if pct > 100 {
		pct = 100
	}

	// 左右留空格
	blankStr := "  "

	// 构建右侧信息（纯文本，用于宽度计算
	// 已完成/总数
	countStr := fmt.Sprintf("%d/%d", d, t)
	// 以下字节/总字节 速率 百分比
	sizeStr := fmt.Sprintf("%s/%s | %s | %d%%", utils.FormatBytes(dsz), utils.FormatBytes(tsz), rate, int(pct))

	// 构建右侧信息（纯文本，用于宽度计算
	var rightStr string
	if t > 0 && tsz > 0 {
		rightStr = fmt.Sprintf("%s | %s | %s%s", countStr, sizeStr, eta, blankStr)
	}
	if t > 0 && tsz == 0 {
		rightStr = countStr + blankStr
	}
	if t == 0 && tsz > 0 {
		rightStr = fmt.Sprintf("%s | %s%s", sizeStr, eta, blankStr)
	}

	// 计算进度条可用宽度（含边框占用的列宽）。
	// 宽度计算使用未着色的纯文本长度，避免 ANSI 序列占位。
	st := pt.style
	bracketW := len(st.LeftBracket) + len(st.RightBracket)
	barArea := wd - len(blankStr) - len(rightStr) - 1 - bracketW
	if barArea < 10 {
		// 空间不足，降级显示
		return colorize(pt.noColor, pt.color.Stats, rightStr)
	}

	bar := buildStyledBar(st, barArea, pct)
	return fmt.Sprintf("%s%s %s", blankStr, bar, colorize(pt.noColor, colorStats, rightStr))
}

// buildStyledBar 按给定样式绘制进度条主体（含边框与着色）。
// barArea 为进度条内部可用的显示列宽，frac 为完成比例 [0,1]。
func buildStyledBar(st *Style, barArea int, frac float64) string {
	fw := len(st.Filled)
	ew := len(st.Empty)
	headW := len(st.Head)

	// 以列宽为单位计算已完成列数
	filledCols := int(frac*float64(barArea) + 0.5)

	// 预留进度头的列宽：未满且有 Head 时，头占 headW 列
	useHead := headW > 0 && filledCols < barArea && filledCols >= headW
	if useHead {
		filledCols -= headW
	}
	emptyCols := barArea - filledCols
	if useHead {
		emptyCols -= headW
	}
	if emptyCols < 0 {
		emptyCols = 0
	}

	var b strings.Builder
	b.WriteString(st.LeftBracket)
	b.WriteString(repeatToWidth(st.Filled, fw, filledCols))
	if useHead {
		b.WriteString(st.Head)
	}
	b.WriteString(repeatToWidth(st.Empty, ew, emptyCols))
	b.WriteString(st.RightBracket)
	return b.String()
}

// repeatToWidth 用显示宽度为 unitW 的字符 unit 填满 cols 个显示列。
// 若 cols 不是 unitW 的整数倍，剩余列用空格补齐，保证总列宽精确等于 cols。
func repeatToWidth(unit string, unitW, cols int) string {
	if cols <= 0 || unit == "" {
		if cols > 0 {
			return strings.Repeat(" ", cols)
		}
		return ""
	}
	if unitW < 1 {
		unitW = 1
	}
	n := cols / unitW
	rem := cols - n*unitW
	var b strings.Builder
	b.Grow(cols)
	for i := 0; i < n; i++ {
		b.WriteString(unit)
	}
	if rem > 0 {
		b.WriteString(strings.Repeat(" ", rem))
	}
	return b.String()
}

// colorize 用给定 ANSI 颜色包裹文本；noColor 时原样返回。
func colorize(noColor bool, color, s string) string {
	if noColor || color == "" {
		return s
	}
	return color + s + ansiReset
}
