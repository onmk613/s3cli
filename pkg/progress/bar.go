package progress

import (
	"fmt"
	"strings"
	"time"

	"s3cli/pkg/utils"
)

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

	// 构建右侧信息
	countStr := fmt.Sprintf("%d/%d", d, t)
	sizeStr := fmt.Sprintf("%s/%s", utils.FormatBytes(dsz), utils.FormatBytes(tsz))
	rightStr := fmt.Sprintf(" %d%% | %s | %s | %s", pct, sizeStr, rate, eta)

	// 计算进度条可用宽度（含边框占用的列宽）
	st := pt.style
	bracketW := displayWidth(st.LeftBracket) + displayWidth(st.RightBracket)
	barArea := wd - len(rightStr) - len(countStr) - 2 - bracketW
	if barArea < 6 {
		// 空间不足，降级显示
		return fmt.Sprintf("%s %d%% | %s", countStr, pct, sizeStr)
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
	return fmt.Sprintf("%s %s%s", bar, countStr, rightStr)
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
	b.WriteString(st.RightBracket)
	if st.BarColor != "" {
		b.WriteString(ansiReset)
	}
	return b.String()
}
