package progress

import (
	"fmt"
	"strings"
	"time"

	"s3cli/pkg/utils"
)

// 进度条右侧文本的颜色（数据色），与左侧 bar 的 style.BarColor 区分。
const (
	colorCount = "\033[1m"  // count（如 17/48）加粗
	colorStats = "\033[36m" // 百分比/大小/速率/ETA 用青色
)

// colorize 用给定 ANSI 颜色包裹文本；noColor 时原样返回。
func colorize(noColor bool, color, s string) string {
	if noColor || color == "" {
		return s
	}
	return color + s + ansiReset
}

// buildBar 构建进度条字符串
func (pt *ProgressTracker) buildBar(wd int) string {
	d := pt.done.Load()
	t := pt.total.Load()
	dsz := pt.doneSz.Load()
	tsz := pt.totalSz.Load()

	elapsed := time.Since(pt.startAt)

	// 计算速率
	rate := "0B/s"
	if elapsed.Seconds() > 0.5 {
		rate = utils.FormatBytes(int64(float64(dsz)/elapsed.Seconds())) + "/s"
	}

	// 计算预计剩余时间 (ETA)
	var eta string
	if d > 0 && t > d && elapsed.Seconds() > 0.5 {
		// 根据当前速率估算剩余时间
		etaSec := elapsed.Seconds() * float64(t-d) / float64(d)
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
	pct := 0
	if t > 0 {
		pct = int(float64(d) * 100 / float64(t))
	}
	if pct > 100 {
		pct = 100
	}

	// 构建右侧信息（纯文本，用于宽度计算）
	countStr := fmt.Sprintf("%d/%d", d, t)
	sizeStr := fmt.Sprintf("%s/%s", utils.FormatBytes(dsz), utils.FormatBytes(tsz))
	rightStr := fmt.Sprintf(" %d%% | %s | %s | %s", pct, sizeStr, rate, eta)

	// 计算进度条可用宽度（含边框占用的列宽）。
	// 宽度计算使用未着色的纯文本长度，避免 ANSI 序列占位。
	st := pt.style
	if pt.noColor {
		st.BarColor = ""
	}
	bracketW := displayWidth(st.LeftBracket) + displayWidth(st.RightBracket)
	barArea := wd - len(rightStr) - len(countStr) - 2 - bracketW
	if barArea < 6 {
		// 空间不足，降级显示
		return colorize(pt.noColor, colorCount, fmt.Sprintf("%s %d%% | %s", countStr, pct, sizeStr))
	}

	frac := 0.0
	if t > 0 {
		frac = float64(d) / float64(t)
	}
	if frac > 1 {
		frac = 1
	}
	if frac < 0 {
		frac = 0
	}

	bar := buildStyledBar(st, barArea, frac)
	// count 用加粗，右侧统计用青色（数据色）；noColor 时均为纯文本。
	return fmt.Sprintf("%s %s%s",
		bar,
		colorize(pt.noColor, colorCount, countStr),
		colorize(pt.noColor, colorStats, rightStr),
	)
}

// buildStyledBar 按给定样式绘制进度条主体（含边框与着色）。
// barArea 为进度条内部可用的显示列宽，frac 为完成比例 [0,1]。
func buildStyledBar(st Style, barArea int, frac float64) string {
	st = st.normalize()

	// 各填充单元的显示宽度（emoji/全角可能 >1）
	fw := displayWidth(st.Filled)
	if fw < 1 {
		fw = 1
	}
	ew := displayWidth(st.Empty)
	if ew < 1 {
		ew = 1
	}
	headW := displayWidth(st.Head) // Head 可为空 -> 0

	// 以列宽为单位计算已完成列数
	filledCols := int(frac*float64(barArea) + 0.5)
	if filledCols > barArea {
		filledCols = barArea
	}
	if filledCols < 0 {
		filledCols = 0
	}

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
	if st.BarColor != "" {
		b.WriteString(st.BarColor)
	}
	b.WriteString(st.LeftBracket)
	b.WriteString(repeatToWidth(st.Filled, fw, filledCols))
	if useHead {
		b.WriteString(st.Head)
	}
	b.WriteString(repeatToWidth(st.Empty, ew, emptyCols))
	b.WriteString(st.RightBracket)
	if st.BarColor != "" {
		b.WriteString(ansiReset)
	}
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
