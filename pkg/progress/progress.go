package progress

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	myprint "s3cli/pkg/fmtutil"

	"golang.org/x/term"
)

// ProgressTracker 终端进度条 + 滚动区域。
//
// 绘制策略（为什么这样设计）：
//   - 仅在进度区所占行数内操作，绝不使用 \033[J（清到屏底）→ 避免删除终端上面的历史输出
//   - 也不使用 \033[s/\033[u（光标保存/恢复）→ 多行变化时光标会错位
//   - 每次重绘：\033[nA 上移到旧进度区顶部 → 逐行 \r\033[K 清除并写入 →
//     进度区缩小/扩大时只补清多余行，光标始终落在进度条行尾。
//   - 所有滚动行强制截断到终端宽度，绝不折行 → 保持布局整齐
//
// 兼容性：依赖 ANSI 转义序列（\033[nA / \033[K / \r）。
// 主流终端均支持。非终端环境自动退化为纯文本输出。

type ProgressTracker struct {
	mu sync.Mutex // 互斥锁，保护并发访问（多个 goroutine 同时更新进度）

	// 输出相关
	termW  io.Writer // 终端输出（带颜色/转义序列）
	rawW   io.Writer // 原始输出（非终端环境，如重定向到文件）
	isTerm bool      // 是否为终端环境（决定是否显示动画进度条）
	termWd int       // 终端宽度（字符数），用于计算进度条长度

	// 统计数据（使用 atomic 类型，支持无锁并发读写）
	total   atomic.Int64 // 总文件数
	done    atomic.Int64 // 已完成文件数
	totalSz atomic.Int64 // 总字节数
	doneSz  atomic.Int64 // 已完成字节数
	failed  atomic.Int64 // 失败文件数

	// 显示相关
	label   string        // 进度条标签（如 "Uploading"）
	startAt time.Time     // 开始时间，用于计算速率和 ETA

	// 滚动缓冲区（显示最近处理的文件名）
	scroll    []string // 滚动消息环形缓冲区
	scrollMax int      // 最大保存条数
	scrollPos int      // 环形写位置（当前写到哪个槽位）
	scrollLen int      // 实际条目数（可能小于 scrollMax）
	lastLines int      // 上次绘制占用了多少行（用于增量更新）
	barWidth  int      // 上次进度条宽度（宽度变化时调整布局）

	started atomic.Bool // 是否已启动（用于控制光标隐藏/显示）
}

// NewProgressTracker 创建一个新的进度跟踪器
// 参数：
//   - w: 输出目标（可以是终端或文件）
//   - scrollMax: 最多显示多少条滚动消息（0 表示不显示滚动区）
//   - label: 进度条标签，如 "Downloading"
func NewProgressTracker(w io.Writer, scrollMax int, label string) *ProgressTracker {
	// 初始化默认值（非终端环境）
	rawW := io.Discard    // 原始输出默认丢弃（不输出）
	termW := w            // 终端输出默认用原 writer
	isTerm := false       // 默认不是终端
	wd := 80              // 默认终端宽度 80 字符

	// 情况1：传入的是 MultiWriter（自定义的多路输出器）
	if mw, ok := w.(*myprint.MultiWriter); ok {
		for _, t := range mw.Targets {
			if t.Color {  // 支持颜色的目标视为终端
				termW = t.W
				isTerm = true
			} else {  // 不支持颜色的目标视为原始输出（文件）
				rawW = t.W
			}
		}
	// 情况2：传入的是普通文件
	} else if f, ok := w.(*os.File); ok {
		isTerm = detectTerm(f)  // 检测是否为终端（如 /dev/tty）
		if isTerm {
			// 获取终端宽度
			if fd := int(f.Fd()); fd >= 0 {
				if tw, _, err := term.GetSize(fd); err == nil && tw > 0 {
					wd = tw
				}
			}
			termW = w
			rawW = io.Discard  // 终端环境下不输出到原始输出
		} else {
			// 不是终端（如重定向到文件），退化为纯文本
			rawW = w
		}
	}

	return &ProgressTracker{
		termW:     termW,
		rawW:      rawW,
		isTerm:    isTerm,
		termWd:    wd,
		scrollMax: scrollMax,
		scroll:    make([]string, 0, max(scrollMax, 1)), // 预分配空间
		label:     label,
		startAt:   time.Now(),
	}
}

// SetContextDone 实现接口要求（当前未使用）
func (pt *ProgressTracker) SetContextDone(_ <-chan struct{}) {}

// Start 启动进度条显示
// 在终端环境下会隐藏光标，避免闪烁
func (pt *ProgressTracker) Start() {
	pt.started.Store(true)  // 标记为已启动
	if pt.isTerm {
		fmt.Fprint(pt.termW, "\033[?25l") // ANSI 转义序列：隐藏光标
	}
}

// Stop 停止进度条，输出最终统计信息
func (pt *ProgressTracker) Stop() {
	// 如果已经停止，直接返回（使用 CompareAndSwap 确保只执行一次）
	if !pt.started.Swap(false) {
		return
	}
	pt.mu.Lock()
	defer pt.mu.Unlock()

	// 清除进度区并显示光标
	if pt.isTerm && pt.lastLines > 0 {
		// 上移到进度区顶部（向上移动 lastLines 行）
		fmt.Fprintf(pt.termW, "\033[%dA", pt.lastLines)
		
		// 逐行清除（不用 \033[J，避免误删上方历史）
		for i := 0; i < pt.lastLines; i++ {
			fmt.Fprint(pt.termW, "\r\033[K")  // \r 回车，\033[K 清除到行尾
			if i < pt.lastLines-1 {
				fmt.Fprint(pt.termW, "\n")    // 换到下一行
			}
		}
		
		// 回到顶部，准备输出总结
		fmt.Fprintf(pt.termW, "\033[%dA", pt.lastLines-1)
		fmt.Fprint(pt.termW, "\033[?25h")  // 显示光标
		pt.lastLines = 0
	}

	// 计算统计信息
	elapsed := time.Since(pt.startAt).Truncate(time.Millisecond)
	d := pt.done.Load()
	t := pt.total.Load()
	dsz := pt.doneSz.Load()
	tsz := pt.totalSz.Load()
	f := pt.failed.Load()
	
	// 计算速率
	rate := "0 B/s"
	if elapsed.Seconds() > 0 {
		rate = myprint.FormatBytes(int64(float64(dsz)/elapsed.Seconds())) + "/s"
	}
	
	// 构建总结信息
	summary := fmt.Sprintf("%s: %d/%d files (%s/%s) in %s (%s)", 
		pt.label, d, t, myprint.FormatBytes(dsz), myprint.FormatBytes(tsz), elapsed, rate)
	if f > 0 {
		summary += fmt.Sprintf(", %d failed", f)
	}
	myprint.Successf("%s\n", summary)
}

// AddTotal 增加总任务数（在开始处理前调用）
func (pt *ProgressTracker) AddTotal(n int64, sz int64) {
	pt.total.Add(n)      // 增加总文件数
	pt.totalSz.Add(sz)   // 增加总字节数
}

// AddDone 增加已完成任务（每完成一个文件调用一次）
func (pt *ProgressTracker) AddDone(n int64, sz int64, msg string) {
	pt.done.Add(n)       // 增加已完成文件数
	pt.doneSz.Add(sz)    // 增加已完成字节数

	pt.mu.Lock()
	defer pt.mu.Unlock()

	// 非终端环境：直接输出纯文本日志
	if pt.rawW != nil && pt.rawW != io.Discard {
		fmt.Fprintln(pt.rawW, msg)
	}

	// 终端环境：更新滚动区和进度条
	if pt.isTerm {
		pt.pushScroll(msg)  // 将消息加入滚动缓冲区
		pt.redraw()         // 重新绘制界面
	}
}

// AddFailed 增加失败计数
func (pt *ProgressTracker) AddFailed() {
	pt.failed.Add(1)
}

// ── 内部实现（私有方法）────────────────────────────────────────

// pushScroll 将新消息加入滚动缓冲区（环形缓冲区）
func (pt *ProgressTracker) pushScroll(msg string) {
	if pt.scrollMax == 0 {
		return  // 不需要滚动区
	}
	
	if pt.scrollLen < pt.scrollMax {
		// 缓冲区未满：直接追加
		pt.scroll = append(pt.scroll, msg)
		pt.scrollLen++
	} else {
		// 缓冲区已满：覆盖最旧的消息（环形覆盖）
		pt.scrollPos = (pt.scrollPos + 1) % pt.scrollMax
		pt.scroll[pt.scrollPos] = msg
	}
}

// scrollOrdered 按顺序返回滚动消息（从旧到新）
func (pt *ProgressTracker) scrollOrdered() []string {
	if pt.scrollLen == 0 {
		return nil
	}
	
	if pt.scrollLen < pt.scrollMax {
		// 未满，直接返回全部
		return pt.scroll
	}
	
	// 已满，需要从环形缓冲区的起始位置开始取
	out := make([]string, 0, pt.scrollLen)
	for i := 0; i < pt.scrollMax; i++ {
		// 计算环形索引（从上次写入位置的下一个开始）
		idx := (pt.scrollPos + 1 + i) % pt.scrollMax
		out = append(out, pt.scroll[idx])
	}
	return out
}

// redraw 重新绘制整个进度界面（终端专用）
func (pt *ProgressTracker) redraw() {
	// 获取最新终端宽度（用户可能调整了窗口大小）
	wd := pt.termWd
	if f, ok := pt.termW.(*os.File); ok {
		if fd := int(f.Fd()); fd >= 0 {
			if tw, _, err := term.GetSize(fd); err == nil && tw > 0 {
				wd = tw
				pt.termWd = wd
			}
		}
	}
	if wd < 20 {
		wd = 80  // 最小宽度保护
	}

	scrollLines := pt.scrollOrdered()
	newLines := len(scrollLines) + 1  // 滚动行数 + 1条进度条

	maxLineW := wd - 2  // 减去 "  " 前缀的宽度
	if maxLineW < 20 {
		maxLineW = 20
	}

	// 构建进度条
	barLine := pt.buildBar(wd)
	lineW := displayWidth(barLine)
	if lineW > pt.barWidth {
		pt.barWidth = lineW
	}

	var sb strings.Builder

	// 1. 上移到旧进度区顶部（首次 lastLines==0 则直接从光标处绘制）
	if pt.lastLines > 0 {
		sb.WriteString(fmt.Sprintf("\033[%dA", pt.lastLines))
	}

	// 2. 逐行清除并绘制滚动行（绝不使用 \033[J）
	for _, line := range scrollLines {
		sb.WriteString("\r\033[K")           // 回车+清除行
		sb.WriteString("  ")                 // 缩进两个空格
		sb.WriteString(truncateMsg(line, maxLineW))  // 截断过长的消息
		sb.WriteByte('\n')                   // 换行
	}

	// 3. 清除并绘制进度条行（不换行，光标留在行尾）
	sb.WriteString("\r\033[K")
	sb.WriteString(barLine)

	// 4. 如果旧内容行数更多，向下逐行清除多余行
	if newLines < pt.lastLines {
		extra := pt.lastLines - newLines
		for i := 0; i < extra; i++ {
			sb.WriteByte('\n')
			sb.WriteString("\r\033[K")
		}
		// 回到进度条行尾
		sb.WriteString(fmt.Sprintf("\033[%dA", extra))
	}

	pt.lastLines = newLines  // 记录本次占用的行数

	fmt.Fprint(pt.termW, sb.String())
}

// buildBar 构建进度条字符串
// 格式：[=====>    ] 50/100 1.2MB/2.4MB 10% | 1.2MB/s | ETA:00:30
func (pt *ProgressTracker) buildBar(wd int) string {
	d := pt.done.Load()
	t := pt.total.Load()
	dsz := pt.doneSz.Load()
	tsz := pt.totalSz.Load()

	elapsed := time.Since(pt.startAt)
	
	// 计算速率
	rate := "0B/s"
	if elapsed.Seconds() > 0.5 {
		rate = myprint.FormatBytes(int64(float64(dsz)/elapsed.Seconds())) + "/s"
	}

	// 计算预计剩余时间 (ETA)
	var eta string
	if d > 0 && t > 0 && elapsed.Seconds() > 0.5 {
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
	
	// 构建右侧信息
	countStr := fmt.Sprintf("%d/%d", d, t)
	sizeStr := fmt.Sprintf("%s/%s", myprint.FormatBytes(dsz), myprint.FormatBytes(tsz))
	rightStr := fmt.Sprintf(" %d%% | %s | %s | %s", pct, sizeStr, rate, eta)

	// 计算进度条可用宽度
	barArea := wd - len(rightStr) - len(countStr) - 4
	if barArea < 6 {
		// 空间不足，降级显示
		return fmt.Sprintf("%s %d%% | %s", countStr, pct, sizeStr)
	}
	
	// 绘制进度条 [=====>    ]
	barLen := barArea - 1
	filled := 0
	if t > 0 {
		filled = int(float64(d) * float64(barLen) / float64(t))
	}
	if filled > barLen {
		filled = barLen
	}
	if filled < 0 {
		filled = 0
	}
	
	var bar strings.Builder
	bar.WriteByte('[')
	for i := 0; i < filled; i++ {
		bar.WriteByte('=')
	}
	if filled < barLen {
		bar.WriteByte('>')  // 当前进度位置
		for i := filled + 1; i < barLen; i++ {
			bar.WriteByte(' ')
		}
	}
	bar.WriteByte(']')
	
	return fmt.Sprintf("%s %s %s", bar.String(), countStr, rightStr)
}

// truncateMsg 截断过长的消息，末尾添加"…"
// 需要考虑中文字符（宽度2）和英文字符（宽度1）
func truncateMsg(msg string, maxLen int) string {
	if maxLen <= 1 {
		return ""
	}
	runes := []rune(msg)
	// 如果总宽度不超过限制，直接返回
	if displayWidth(msg) <= maxLen {
		return msg
	}
	
	// 逐字符累加宽度，找到截断点
	limit := maxLen - 1  // 留一个位置给"…"
	target := 0
	cut := 0
	for i, r := range runes {
		// 中文字符宽度2，英文宽度1
		w := 1
		if r > 0x7FF {
			w = 2
		}
		if target+w > limit {
			break
		}
		target += w
		cut = i + 1
	}
	if cut == 0 {
		return "…"
	}
	return string(runes[:cut]) + "…"
}

// displayWidth 计算字符串在终端中的显示宽度
// 简单规则：ASCII字符宽度1，中文字符宽度2
func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		if r > 0x7FF {  // 大于0x7FF的是中文等宽字符
			w += 2
		} else {
			w++
		}
	}
	return w
}

// detectTerm 检测文件描述符是否为终端
func detectTerm(f *os.File) bool {
	if f == nil {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// max 返回两个整数中的最大值
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}