package progress

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/term"
)

const (
	// 控制颜色输出
	ansiReset = "\033[0m"
	// 统计信息颜色, 加粗绿色
	colorStats = "\033[1;32m"
	// 错误信息输出颜色, 红色
	colorError = "\033[31m"
	// 完成信息颜色, 加粗蓝色
	colorDone = "\033[1;34m"
	// 单个进度条, 清理当前终端两行并把光标移到行首
	clearLine = "\r\033[2K"
)

type Tracker struct {
	mu    sync.Mutex
	width int // 终端宽度（缓存，避免每次渲染都调用 ioctl）

	// 显示节奏控制（均在 mu 保护下）
	lastRender     int64     // 上次渲染的 unix nano，用于节流
	widthCheckedAt time.Time // 上次刷新终端宽度的时间

	// 统计数据（使用 atomic 类型，支持无锁并发读写）
	total         atomic.Int64 // 总任务数
	done          atomic.Int64 // 已完成任务数
	totalSz       atomic.Int64 // 总字节数
	doneSz        atomic.Int64 // 已完成字节数
	failed        atomic.Int64 // 失败任务计数
	failedStrings []string     // 失败任务列表, 结束后打印

	// 显示相关（除 quiet 外均在 mu 保护下读写）
	label   string      // 进度条标签（如 "Uploading"）
	startAt time.Time   // 开始时间，用于计算总耗时
	style   *Style      // 进度条填充物
	color   *Colors     // 统计信息颜色
	quiet   atomic.Bool // 直接输出原始信息而不显示进度条
}

// New 创建一个新的进度条
func New() *Tracker {
	pt := &Tracker{
		style:          DefaultStyle(),
		label:          "Uploading",
		color:          DefaultColors(),
		widthCheckedAt: time.Now(),
	}
	// 获取终端宽度
	if tw, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && tw > 0 {
		pt.width = tw
	} else {
		// 不是终端或者获取失败, 输出原始内容
		pt.quiet.Store(true)
	}
	return pt
}

// SetStyle 自定义进度条填充物
func (pt *Tracker) SetStyle(s *Style) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	if s == nil {
		s = DefaultStyle()
	}
	if s.Filled == "" {
		s.Filled = "="
	}
	if s.Empty == "" {
		s.Empty = " "
	}
	pt.style = s
}

// SetLabel 自定义进度条标签
func (pt *Tracker) SetLabel(label string) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.label = label
}

// SetColor 自定义颜色风格
func (pt *Tracker) SetColor(color *Colors) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	if color == nil {
		pt.color.Done = ""
		pt.color.Error = ""
		pt.color.Stats = ""
		return
	}
	if color.Stats == "" {
		color.Stats = colorStats
	}
	if color.Error == "" {
		color.Error = colorError
	}
	if color.Done == "" {
		color.Done = colorDone
	}
	pt.color = color
}

// SetQuiet 设置为静默模式，直接输出原始信息而不显示进度条
func (pt *Tracker) SetQuiet() {
	pt.quiet.Store(true)
}

// Start 主要为开始计时
func (pt *Tracker) Start() {
	pt.mu.Lock()
	pt.startAt = time.Now()
	pt.mu.Unlock()
}

// Stop 输出最终统计信息
// 一般配合 defer pt.Stop()
func (pt *Tracker) Stop() {
	// 在锁内：强制刷新最后一帧（显示真实终态），并快照后续打印所需字段
	pt.mu.Lock()
	if !pt.quiet.Load() {
		pt.render(true)
	}
	startAt := pt.startAt
	label := pt.label
	doneColor := pt.color
	failedStrings := append([]string(nil), pt.failedStrings...)
	pt.mu.Unlock()

	// 计算统计信息
	elapsed := time.Since(startAt).Truncate(time.Millisecond)
	d := pt.done.Load()
	t := pt.total.Load()
	dsz := pt.doneSz.Load()
	tsz := pt.totalSz.Load()
	f := pt.failed.Load()

	// 计算速率
	rate := "0 B/s"
	if elapsed.Seconds() > 0 {
		rate = formatBytes(int64(float64(dsz)/elapsed.Seconds())) + "/s"
	}

	// 构建总结信息
	summary := fmt.Sprintf("%s: %d/%d total (%s/%s) in %s (%s)",
		label, d, t, formatBytes(dsz), formatBytes(tsz), elapsed, rate)

	// 如果有失败的任务
	if f > 0 {
		summary += fmt.Sprintf(", %d failed", f)
	}

	// 如果设置了颜色输出
	if doneColor != nil && doneColor.Done != "" {
		summary = doneColor.Done + summary + ansiReset
	}

	// 换行打印统计信息
	_, _ = fmt.Fprintln(os.Stdout, "\n "+summary)

	// 打印失败任务列表
	for _, s := range failedStrings {
		str := fmt.Sprintf(" failed: %s", s)
		if doneColor != nil && doneColor.Error != "" {
			str = doneColor.Error + str + ansiReset
		}
		_, _ = fmt.Fprintln(os.Stderr, str)
	}
}
