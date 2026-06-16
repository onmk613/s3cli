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

	// 构建右侧信息（纯文本，用于宽度计算
	// 已完成/总数
	countStr := fmt.Sprintf("%d/%d", d, t)
	// 以下字节/总字节 速率 百分比
	sizeStr := fmt.Sprintf("%s/%s | %s | %d%%", utils.FormatBytes(dsz), utils.FormatBytes(tsz), rate, int(pct))

	// 构建右侧信息（纯文本，用于宽度计算
	var rightStr string
	if t > 0 && tsz > 0 {
		rightStr = fmt.Sprintf("%s | %s | %s", countStr, sizeStr, eta)
	}
	if t > 0 && tsz == 0 {
		rightStr = countStr
	}
	if t == 0 && tsz > 0 {
		rightStr = fmt.Sprintf("%s | %s", sizeStr, eta)
	}

	// 左/右/中留空格
	blankStr := "  "

	// 计算进度条可用宽度（含边框占用的列宽）。
	// 宽度计算使用未着色的纯文本长度，避免 ANSI 序列占位。
	st := pt.style
	bracketW := len(st.LeftBracket) + len(st.RightBracket)
	barArea := wd - len(blankStr)*3 - len(rightStr) - bracketW
	if barArea < 10 {
		// 空间不足，降级显示
		return colorize(pt.noColor, pt.color.Stats, rightStr)
	}

	// 注意：buildStyledBar 期望比例 [0,1]，pct 是百分比 [0,100]，必须归一化，
	// 否则 filledCols 会放大 100 倍，进度条远超终端宽度而折行、堆叠。
	bar := buildStyledBar(st, barArea, pct/100)

	return fmt.Sprintf("%s", colorize(
		pt.noColor,
		colorStats,
		fmt.Sprintf("%s%s%s%s%s", blankStr, bar, blankStr, rightStr, blankStr)))
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
func colorize(noColor bool, color, s string) string {
	if noColor || color == "" {
		return s
	}
	return color + s + ansiReset
}
