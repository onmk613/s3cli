package progress

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"s3cli/pkg/utils"

	"golang.org/x/term"
)

const (
	defaultBarWidth = 80
)

type ProgressTracker struct {
	mu sync.Mutex

	// 输出相关
	output io.Writer // 终端输出
	width  int       // 终端宽度
	fd     int       // 缓存文件描述符，避免重复 Fd() 调用
	Color  string    // 颜色

	// 统计数据（使用 atomic 类型，支持无锁并发读写）
	total         atomic.Int64 // 总任务数
	done          atomic.Int64 // 已完成任务数
	totalSz       atomic.Int64 // 总字节数
	doneSz        atomic.Int64 // 已完成字节数
	failed        atomic.Int64 // 失败任务计数
	failedStrings []string     // 失败任务列表, 结束后打印

	// 显示相关
	label   string    // 进度条标签（如 "Uploading"）
	startAt time.Time // 开始时间，用于计算速率和 ETA
	style   Style     // 进度条填充物

	sigCh chan os.Signal // 信号兜底：被中断时恢复光标
}

func New() *ProgressTracker {
	return &ProgressTracker{
		output:  os.Stdout,
		fd:      int(os.Getegid()),
		width:   defaultBarWidth,
		startAt: time.Now(),
		style:   DefaultStyle(),
	}
}

// SetStyle 设置进度条样式（填充物 + 颜色）
func (pt *ProgressTracker) SetStyle(s Style) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.style = s.normalize()
}

func (pt *ProgressTracker) SetLabel(label string) {
	pt.label = label
}

func (pt *ProgressTracker) SetColor(color string) {
	pt.Color = color
}

func (pt *ProgressTracker) SetWriter(w io.Writer) error {
	f, ok := w.(*os.File)
	if !ok {
		return fmt.Errorf("unsupported writer type: %T", w)
	}

	if !term.IsTerminal(int(f.Fd())) {
		return fmt.Errorf("terminal is not detected")
	}

	fd := int(f.Fd())
	if fd >= 0 {
		if tw, _, err := term.GetSize(fd); err == nil && tw > 0 {
			pt.width = tw
		}
	}
	pt.output = w

	return nil
}

// Start 启动进度条显示。终端环境下隐藏光标以减少闪烁。
func (pt *ProgressTracker) Start() {
	pt.sigCh = make(chan os.Signal, 1)
	signal.Notify(pt.sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func(ch chan os.Signal) {
		if _, ok := <-ch; !ok {
			return
		}
		// 被中断：清除进度条行，避免残留半截彩色进度条。
		pt.mu.Lock()
		fmt.Fprint(pt.output, "\r\033[K")
		pt.mu.Unlock()
	}(pt.sigCh)
}

// Stop 停止进度条，输出最终统计信息
func (pt *ProgressTracker) Stop() {
	// 注销信号兜底并唤醒/结束兜底 goroutine。
	if pt.sigCh != nil {
		signal.Stop(pt.sigCh)
		close(pt.sigCh)
		pt.sigCh = nil
	}

	pt.mu.Lock()
	defer pt.mu.Unlock()

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
		rate = utils.FormatBytes(int64(float64(dsz)/elapsed.Seconds())) + "/s"
	}

	// 构建总结信息
	summary := fmt.Sprintf("%s: %d/%d total (%s/%s) in %s (%s)",
		pt.label, d, t, utils.FormatBytes(dsz), utils.FormatBytes(tsz), elapsed, rate)
	if f > 0 {
		summary += fmt.Sprintf(", %d failed", f)
	}

	// 终端环境下先清除进度条行，再输出最终统计。
	fmt.Fprint(pt.output, "\r\033[K")
	fmt.Fprintln(pt.output, summary)

	// 打印失败任务列表
	for _, s := range pt.failedStrings {
		fmt.Fprintln(pt.output, "  failed:", s)
	}
}
