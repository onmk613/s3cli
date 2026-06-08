package progress

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	myprint "s3cli/pkg/fmtutil"

	"golang.org/x/term"
)

// 进度区使用的 ANSI 颜色（直接写入终端，不经 fmtutil）。
const (
	colorBar    = "\033[32m" // 进度条：绿色
	colorScroll = "\033[2m"  // 滚动记录：暗淡（灰色）
	colorReset  = "\033[0m"
)

// ProgressTracker 终端进度条 + 固定滚动窗口。
//
// 绘制策略（为什么这样设计）：
//
//	采用「固定行数进度区」方案：进度区始终占 scrollMax+1 行
//	（上方 scrollMax 行为最近消息滚动窗口，最后 1 行为进度条）。
//
//	关键：第一次绘制时先输出 scrollMax+1 个空行把区域「预占」住，
//	      让终端在此时一次性完成向上滚动；此后每次重绘都是
//	      「\033[nA 上移固定 n 行 → 逐行 \r\033[K 重写」，行数恒定不变。
//
//	早期「随消息增长的多行进度区」方案会崩坏：进度区行数边画边涨，
//	在终端底部 \033[nA 上移被截断 → 行数失配 → 进度条堆叠、吃掉历史。
//	本方案行数固定且区域已预占，从根本上消除该问题。
//
//	滚动窗口用环形缓冲保存最近 scrollMax 条消息；旧消息滚出窗口即被覆盖，
//	不会逐条刷屏，也不污染上方的历史输出。scrollMax==0 时只显示进度条单行。
//
// 兼容性：依赖 \033[nA（上移）、\r、\033[K（清行），主流终端均支持。
// 非终端环境（重定向到文件）自动退化为纯文本逐行输出。

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
	label   string    // 进度条标签（如 "Uploading"）
	startAt time.Time // 开始时间，用于计算速率和 ETA

	// 固定滚动窗口（环形缓冲，保存最近 scrollMax 条消息）
	scrollMax int      // 滚动窗口行数；0 表示只显示进度条单行
	scroll    []string // 环形缓冲，长度 == scrollMax
	scrollPos int      // 下一个写入位置
	scrollLen int      // 已写入条数（<=scrollMax）
	drawn     bool     // 进度区是否已预占/绘制过（决定是否需要上移重绘）

	sigCh   chan os.Signal // 信号兜底：被中断时恢复光标
	started atomic.Bool    // 是否已启动（用于控制光标隐藏/显示）
	aborted atomic.Bool    // 被 Ctrl+C 中断后置位：此后所有重绘都跳过，避免清屏后又被重画
}

// NewProgressTracker 创建一个新的进度跟踪器
// 参数：
//   - w: 输出目标（可以是终端或文件）
//   - scrollMax: 最多显示多少条滚动消息（0 表示不显示滚动区）
//   - label: 进度条标签，如 "Downloading"
func NewProgressTracker(w io.Writer, scrollMax int, label string) *ProgressTracker {
	// 初始化默认值（非终端环境）
	rawW := io.Discard // 原始输出默认丢弃（不输出）
	termW := w         // 终端输出默认用原 writer
	isTerm := false    // 默认不是终端
	wd := 80           // 默认终端宽度 80 字符

	// 情况1：传入的是 MultiWriter（自定义的多路输出器）
	if mw, ok := w.(*myprint.MultiWriter); ok {
		for _, t := range mw.Targets {
			if t.Color { // 支持颜色的目标视为终端
				termW = t.W
				isTerm = true
			} else { // 不支持颜色的目标视为原始输出（文件）
				rawW = t.W
			}
		}
		// 情况2：传入的是普通文件
	} else if f, ok := w.(*os.File); ok {
		isTerm = detectTerm(f) // 检测是否为终端（如 /dev/tty）
		if isTerm {
			// 获取终端宽度
			if fd := int(f.Fd()); fd >= 0 {
				if tw, _, err := term.GetSize(fd); err == nil && tw > 0 {
					wd = tw
				}
			}
			termW = w
			rawW = io.Discard // 终端环境下不输出到原始输出
		} else {
			// 不是终端（如重定向到文件），退化为纯文本
			rawW = w
		}
	}

	pt := &ProgressTracker{
		termW:     termW,
		rawW:      rawW,
		isTerm:    isTerm,
		termWd:    wd,
		scrollMax: scrollMax,
		label:     label,
		startAt:   time.Now(),
	}
	if scrollMax > 0 {
		pt.scroll = make([]string, scrollMax)
	}
	return pt
}

// SetContextDone 实现接口要求（当前未使用）
func (pt *ProgressTracker) SetContextDone(_ <-chan struct{}) {}

// Start 启动进度条显示
// 在终端环境下会隐藏光标，避免闪烁
func (pt *ProgressTracker) Start() {
	pt.started.Store(true) // 标记为已启动
	if pt.isTerm {
		// 加锁，避免隐藏光标的转义序列与首次绘制的输出交错
		pt.mu.Lock()
		fmt.Fprint(pt.termW, "\033[?25l") // ANSI 转义序列：隐藏光标
		pt.mu.Unlock()

		// 信号兜底：即使进程被 Ctrl+C / kill 打断、defer Stop() 来不及执行，
		// 也要在退出前恢复光标，避免终端光标一直消失（看起来像“输出没了”）。
		pt.sigCh = make(chan os.Signal, 1)
		signal.Notify(pt.sigCh, syscall.SIGINT, syscall.SIGTERM)
		go func(ch chan os.Signal) {
			if _, ok := <-ch; !ok {
				return // channel 被 Stop() 关闭，正常退出
			}
			// 收到中断信号：先置位 aborted，阻止后续任何 render() 重绘进度区，
			// 再清除整个进度区并恢复光标，避免“清屏后又被在途的 AddDone 重画”导致错乱。
			// 收尾（summary 输出）统一交给随后执行的 Stop()，这里不额外换行。
			pt.aborted.Store(true)
			pt.mu.Lock()
			pt.clearArea()
			pt.mu.Unlock()
		}(pt.sigCh)
	}
}

// Stop 停止进度条，输出最终统计信息
func (pt *ProgressTracker) Stop() {
	// 如果已经停止，直接返回（使用 CompareAndSwap 确保只执行一次）
	if !pt.started.Swap(false) {
		return
	}

	// 注销信号兜底并唤醒/结束兜底 goroutine。
	if pt.sigCh != nil {
		signal.Stop(pt.sigCh)
		close(pt.sigCh)
		pt.sigCh = nil
	}

	pt.mu.Lock()
	defer pt.mu.Unlock()

	// 清除整个进度区，summary 将从进度区顶部输出。
	pt.clearArea()

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
	pt.total.Add(n)    // 增加总文件数
	pt.totalSz.Add(sz) // 增加总字节数
}

// AddDone 增加已完成任务（每完成一个文件调用一次）
func (pt *ProgressTracker) AddDone(n int64, sz int64, msg string) {
	pt.done.Add(n)    // 增加已完成文件数
	pt.doneSz.Add(sz) // 增加已完成字节数

	pt.mu.Lock()
	defer pt.mu.Unlock()

	// 非终端环境：直接输出纯文本日志
	if pt.rawW != nil && pt.rawW != io.Discard {
		fmt.Fprintln(pt.rawW, msg)
	}

	// 已被中断：不再触碰终端，避免清屏后又被重绘导致输出错乱。
	if !pt.isTerm || pt.aborted.Load() {
		return
	}

	// 把消息写入环形滚动窗口（仅当 scrollMax>0）。
	if pt.scrollMax > 0 {
		pt.scroll[pt.scrollPos] = msg
		pt.scrollPos = (pt.scrollPos + 1) % pt.scrollMax
		if pt.scrollLen < pt.scrollMax {
			pt.scrollLen++
		}
	}

	pt.render()
}

// Tick 仅刷新进度条/速率/ETA（不新增消息）。可用于周期性更新。
func (pt *ProgressTracker) Tick() {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	if pt.isTerm && !pt.aborted.Load() {
		pt.render()
	}
}

// render 重绘整个进度区（固定 scrollMax+1 行）。调用方须持有 pt.mu。
//
// 布局：第 1 行为进度条（绿色），下方 scrollMax 行为滚动窗口（灰色）。
//
// 首次：输出 scrollMax+1 个空行预占区域（让终端一次性完成滚动），
//
//	再上移回顶部绘制；此后区域行数恒定，每次都是上移→逐行重写。
func (pt *ProgressTracker) render() {
	if pt.aborted.Load() {
		return // 已中断，不再绘制
	}
	wd := pt.curWidth()
	maxLineW := wd - 2
	if maxLineW < 20 {
		maxLineW = 20
	}
	lines := pt.scrollMax + 1 // 进度区总行数（进度条 + 滚动窗口）

	var sb strings.Builder

	if !pt.drawn {
		// 预占：输出 lines 个空行，使终端在此刻完成向上滚动，
		// 之后进度区位置固定，\033[nA 上移不会再被截断。
		for i := 0; i < lines; i++ {
			sb.WriteString("\n")
		}
		pt.drawn = true
	}

	// 上移到进度区顶部行首。
	sb.WriteString("\r")
	if lines > 1 {
		sb.WriteString(fmt.Sprintf("\033[%dA", lines-1))
	}

	// 第 1 行：进度条（绿色）。
	sb.WriteString("\r\033[K")
	sb.WriteString(colorBar)
	sb.WriteString(pt.buildBar(wd))
	sb.WriteString(colorReset)

	// 下方 scrollMax 行：滚动窗口（灰色，从旧到新；不足处留空行）。
	if pt.scrollMax > 0 {
		ordered := pt.scrollOrdered()
		for i := 0; i < pt.scrollMax; i++ {
			sb.WriteString("\n\r\033[K")
			if i < len(ordered) {
				sb.WriteString(colorScroll)
				sb.WriteString("  ")
				sb.WriteString(truncateMsg(ordered[i], maxLineW))
				sb.WriteString(colorReset)
			}
		}
	}

	fmt.Fprint(pt.termW, sb.String())
}

// clearArea 清除整个进度区（scrollMax+1 行）并恢复光标。调用方须持有 pt.mu。
//
// 绘制结束时光标停在进度区最后一行行尾，因此先上移 lines-1 行到顶部，
// 逐行清除，最后回到进度区顶部行首（后续输出将从这里覆盖原位置）。
// 幂等：drawn==false 时直接返回，可被 Stop() 与信号兜底安全地重复调用。
func (pt *ProgressTracker) clearArea() {
	if !pt.isTerm || !pt.drawn {
		return
	}
	lines := pt.scrollMax + 1
	var sb strings.Builder
	sb.WriteString("\r") // 当前在最后一行行首
	if lines > 1 {
		sb.WriteString(fmt.Sprintf("\033[%dA", lines-1)) // 上移到顶部
	}
	for i := 0; i < lines; i++ {
		sb.WriteString("\r\033[K") // 清当前行
		if i < lines-1 {
			sb.WriteString("\n")
		}
	}
	// 回到进度区顶部行首。
	if lines > 1 {
		sb.WriteString(fmt.Sprintf("\033[%dA", lines-1))
	}
	sb.WriteString("\r\033[?25h") // 行首 + 显示光标
	fmt.Fprint(pt.termW, sb.String())
	pt.drawn = false
}

// scrollOrdered 按从旧到新的顺序返回滚动窗口内的消息。
func (pt *ProgressTracker) scrollOrdered() []string {
	if pt.scrollLen == 0 {
		return nil
	}
	out := make([]string, 0, pt.scrollLen)
	if pt.scrollLen < pt.scrollMax {
		// 未满：[0, scrollLen) 即为从旧到新。
		out = append(out, pt.scroll[:pt.scrollLen]...)
		return out
	}
	// 已满：从 scrollPos（最旧）开始环形遍历。
	for i := 0; i < pt.scrollMax; i++ {
		idx := (pt.scrollPos + i) % pt.scrollMax
		out = append(out, pt.scroll[idx])
	}
	return out
}

// AddFailed 增加失败计数
func (pt *ProgressTracker) AddFailed() {
	pt.failed.Add(1)
}

// ── 内部实现（私有方法）────────────────────────────────────────

// curWidth 获取当前终端宽度（用户可能调整了窗口大小）。
func (pt *ProgressTracker) curWidth() int {
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
		wd = 80 // 最小宽度保护
	}
	return wd
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
		bar.WriteByte('>') // 当前进度位置
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
	limit := maxLen - 1 // 留一个位置给"…"
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
		if r > 0x7FF { // 大于0x7FF的是中文等宽字符
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
