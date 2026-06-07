package fmtutil

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"golang.org/x/term"
)

// ProgressTracker 终端进度条 + 滚动刷屏日志。
//
// 使用方式（以 put 为例）:
//
//	pt := NewProgressTracker(output, 5, "put")
//	pt.Start()
//	defer pt.Stop()
//
//	// 流式扫描发现文件时:
//	pt.AddTotal(1, fileSize)
//
//	// 完成一个文件时:
//	pt.AddDone(1, fileSize, "local/file.txt → s3://bucket/file.txt")
//
// 终端渲染 ANSI 进度条；非终端目标仅写纯文本日志。
type ProgressTracker struct {
	mu sync.Mutex

	// 目标 writer（如果是 multiWriter 则取第一个支持终端的）
	termW   io.Writer
	rawW    io.Writer // 原始 writer（文件等非终端输出用）
	isTerm  bool
	termWd  int // 终端宽度（列）
	widthFn func() int

	// 统计（atomic 避免锁竞争影响渲染性能）
	total   atomic.Int64
	done    atomic.Int64
	totalSz atomic.Int64
	doneSz  atomic.Int64
	failed  atomic.Int64

	// 滚动缓冲区
	scroll    []string
	scrollMax int // 0 = 显示全部
	scrollPos int // 环形写入位置
	scrollLen int // 实际条目数

	// 控制
	label   string
	startAt time.Time
	ctxDone <-chan struct{}
	ticker  *time.Ticker
	stopCh  chan struct{}
	running atomic.Bool

	// 渲染状态
	lastLines int // 上次渲染行数（用于光标上移）
}

// NewProgressTracker 创建进度条。
//
//	w:          输出 writer（fmtutil.output）
//	scrollMax:  滚动刷屏最大条数，0=全部显示
//	label:      操作名称（put/get/cp/mv/mirror/diff）
func NewProgressTracker(w io.Writer, scrollMax int, label string) *ProgressTracker {
	rawW := w
	termW := w
	isTerm := false
	wd := 80

	// 若是 multiWriter，拆分终端 / 文件输出
	if mw, ok := w.(*multiWriter); ok {
		rawW = io.Discard
		for _, t := range mw.targets {
			if t.color {
				termW = t.w
				isTerm = true
			} else {
				rawW = t.w // 取第一个非终端目标为文件输出
			}
		}
	} else if f, ok := w.(*os.File); ok {
		// os.Stdout / os.Stderr
		isTerm = detectTerm(f)
		if isTerm {
			if fd := int(f.Fd()); fd >= 0 {
				if w, _, err := term.GetSize(fd); err == nil && w > 0 {
					wd = w
				}
			}
			termW = w
		}
		rawW = w
	} else {
		// 其他 writer，不启用终端特性
		rawW = w
	}

	pt := &ProgressTracker{
		termW:     termW,
		rawW:      rawW,
		isTerm:    isTerm,
		termWd:    wd,
		scrollMax: scrollMax,
		scroll:    make([]string, 0, max(scrollMax, 16)),
		label:     label,
		startAt:   time.Now(),
		stopCh:    make(chan struct{}),
	}
	return pt
}

// SetContextDone 接受 context.Done() 信号，ctx 取消时自动 Stop。
func (pt *ProgressTracker) SetContextDone(done <-chan struct{}) {
	pt.ctxDone = done
}

// Start 启动渲染协程（200ms 周期）。
func (pt *ProgressTracker) Start() {
	if pt.running.Swap(true) {
		return
	}
	pt.ticker = time.NewTicker(200 * time.Millisecond)

	// 渲染时检测终端宽度
	if pt.isTerm {
		pt.widthFn = func() int {
			if f, ok := pt.termW.(*os.File); ok {
				fd := int(f.Fd())
				if w, _, err := term.GetSize(fd); err == nil && w > 0 {
					return w
				}
			}
			return pt.termWd
		}
	}

	go func() {
		for {
			select {
			case <-pt.ticker.C:
				pt.render()
			case <-pt.ctxDone:
				pt.Stop()
				return
			case <-pt.stopCh:
				pt.ticker.Stop()
				return
			}
		}
	}()
}

// Stop 停止渲染，输出最终统计。
func (pt *ProgressTracker) Stop() {
	if !pt.running.Swap(false) {
		return
	}
	close(pt.stopCh)

	pt.mu.Lock()
	defer pt.mu.Unlock()

	// 清除最后渲染的内容
	if pt.isTerm && pt.lastLines > 0 {
		// 先换行离开进度条区域
		fmt.Fprint(pt.termW, "\n")
		pt.lastLines = 0
	}

	// 输出最终统计
	elapsed := time.Since(pt.startAt).Truncate(time.Millisecond)
	d := pt.done.Load()
	t := pt.total.Load()
	dsz := pt.doneSz.Load()
	tsz := pt.totalSz.Load()
	f := pt.failed.Load()

	rate := "0 B/s"
	if elapsed.Seconds() > 0 {
		rate = FormatBytes(int64(float64(dsz)/elapsed.Seconds())) + "/s"
	}

	summary := fmt.Sprintf("%s: %d/%d files (%s/%s) in %s (%s)",
		pt.label, d, t, FormatBytes(dsz), FormatBytes(tsz), elapsed, rate)
	if f > 0 {
		summary += fmt.Sprintf(", %d failed", f)
	}
	Successf("%s\n", summary)

	pt.lastLines = 0
}

// AddTotal 增加总文件计数和总大小（扫描发现新文件时调用）。
func (pt *ProgressTracker) AddTotal(n int64, sz int64) {
	pt.total.Add(n)
	pt.totalSz.Add(sz)
}

// AddDone 记录完成一个文件。
//
//	msg: 完成信息（如 "local/a.txt → s3://b/a.txt (1.2MB)"）
func (pt *ProgressTracker) AddDone(n int64, sz int64, msg string) {
	pt.done.Add(n)
	pt.doneSz.Add(sz)

	pt.mu.Lock()
	defer pt.mu.Unlock()

	// 写入文件输出（纯文本，每行完整记录）
	pt.writeRaw(msg + "\n")

	// 若为终端，加入滚动缓冲区
	if pt.isTerm {
		pt.appendScroll(msg)
	}
}

// AddFailed 记录一个失败。
func (pt *ProgressTracker) AddFailed() {
	pt.failed.Add(1)
}

// ---- 内部方法 ----

func (pt *ProgressTracker) appendScroll(msg string) {
	if pt.scrollMax == 0 {
		// 全部显示：直接追加
		pt.scroll = append(pt.scroll, msg)
		pt.scrollLen = len(pt.scroll)
		return
	}

	// 环形缓冲区
	if pt.scrollLen < pt.scrollMax {
		// 还在增长
		pt.scroll = append(pt.scroll, msg)
		pt.scrollLen++
	} else {
		pt.scrollPos = (pt.scrollPos + 1) % pt.scrollMax
		pt.scroll[pt.scrollPos] = msg
	}
}

func (pt *ProgressTracker) writeRaw(s string) {
	if pt.rawW != nil && pt.rawW != io.Discard {
		_, _ = pt.rawW.Write([]byte(s))
	}
}

func (pt *ProgressTracker) render() {
	if !pt.isTerm {
		return
	}

	pt.mu.Lock()
	defer pt.mu.Unlock()

	// 检测终端宽度
	if pt.widthFn != nil {
		pt.termWd = pt.widthFn()
	}
	wd := pt.termWd
	if wd < 20 {
		wd = 80
	}

	// 构建进度条行
	barLine := pt.buildBar(wd)

	// 构建滚动行
	var scrollLines []string
	if pt.scrollMax == 0 {
		// 全部显示（取最后 10 条防止刷屏）
		start := 0
		if pt.scrollLen > 10 {
			start = pt.scrollLen - 10
		}
		scrollLines = pt.scroll[start:]
	} else {
		scrollLines = pt.scrollOrdered()
	}

	var sb strings.Builder

	// 方法：先上移 lastLines 行，清屏，再绘制
	if pt.lastLines > 0 {
		// 上移 lastLines 行
		sb.WriteString(fmt.Sprintf("\033[%dA", pt.lastLines))
	}

	// 绘制滚动行
	for _, line := range scrollLines {
		sb.WriteString("\r\033[K  ")
		sb.WriteString(truncateMsg(line, wd-4))
		sb.WriteByte('\n')
	}

	// 绘制进度条行
	sb.WriteString("\r\033[K")
	sb.WriteString(barLine)

	// 总行数
	pt.lastLines = len(scrollLines) + 1

	fmt.Fprint(pt.termW, sb.String())
}

func (pt *ProgressTracker) buildBar(wd int) string {
	d := pt.done.Load()
	t := pt.total.Load()
	dsz := pt.doneSz.Load()
	tsz := pt.totalSz.Load()

	elapsed := time.Since(pt.startAt)
	rate := "0B/s"
	if elapsed.Seconds() > 0.5 {
		rate = FormatBytes(int64(float64(dsz)/elapsed.Seconds())) + "/s"
	}

	var eta string
	if d > 0 && t > 0 && elapsed.Seconds() > 0.5 {
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

	// 百分比
	pct := 0
	if t > 0 {
		pct = int(float64(d) * 100 / float64(t))
	}

	// 文件计数部分
	countStr := fmt.Sprintf("%d/%d", d, t)
	// 大小部分
	sizeStr := fmt.Sprintf("%s/%s", FormatBytes(dsz), FormatBytes(tsz))
	// 右侧信息
	rightStr := fmt.Sprintf(" %d%% | %s | %s | %s", pct, sizeStr, rate, eta)

	// 计算进度条可用宽度
	barArea := wd - len(rightStr) - len(countStr) - 4
	if barArea < 6 {
		// 太窄，只显示精简版
		return fmt.Sprintf("%s %d%% | %s", countStr, pct, sizeStr)
	}

	// 绘制进度条
	barLen := barArea - 1 // 留 1 字符间隙
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
		bar.WriteByte('>')
		for i := filled + 1; i < barLen; i++ {
			bar.WriteByte(' ')
		}
	}
	bar.WriteByte(']')

	return fmt.Sprintf("%s %s %s", bar.String(), countStr, rightStr)
}

// scrollOrdered 按时间顺序返回滚动缓冲区条目。
func (pt *ProgressTracker) scrollOrdered() []string {
	if pt.scrollLen == 0 {
		return nil
	}
	if pt.scrollLen < pt.scrollMax {
		return pt.scroll
	}
	// 环形：从 pos+1 开始取到末尾，再从 0 取到 pos
	out := make([]string, 0, pt.scrollLen)
	for i := 0; i < pt.scrollMax; i++ {
		idx := (pt.scrollPos + 1 + i) % pt.scrollMax
		out = append(out, pt.scroll[idx])
	}
	return out
}

// truncateMsg 截断过长消息（考虑 ANSI 颜色码开销）
func truncateMsg(msg string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	// 简单实现：按 rune 计数
	runes := []rune(msg)
	if len(runes) <= maxLen {
		return msg
	}

	// 尝试智能截断：找分隔符
	cut := maxLen - 1
	if idx := strings.LastIndex(string(runes[:maxLen]), " "); idx > maxLen/2 {
		cut = utf8.RuneCountInString(string(runes[:idx]))
	}
	return string(runes[:cut]) + "…"
}

// ---- 辅助 ----

func detectTerm(f *os.File) bool {
	if f == nil {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
