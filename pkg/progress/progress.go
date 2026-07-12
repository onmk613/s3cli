package progress

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"s3cli/pkg/utils"

	"golang.org/x/term"
)

const (
	// 控制颜色输出的 ANSI 转义码
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
	width int // 终端宽度

	// 统计数据（使用 atomic 类型，支持无锁并发读写）
	total         atomic.Int64 // 总任务数
	done          atomic.Int64 // 已完成任务数
	totalSz       atomic.Int64 // 总字节数
	doneSz        atomic.Int64 // 已完成字节数
	failed        atomic.Int64 // 失败任务计数
	failedStrings []string     // 失败任务列表, 结束后打印

	// 显示相关
	label   string    // 进度条标签（如 "Uploading"）
	startAt time.Time // 开始时间，用于计算总耗时
	style   *Style    // 进度条填充物
	color   *Colors   // 统计信息颜色
	quiet   bool      // 直接输出原始信息
}

func New() *Tracker {
	pt := &Tracker{
		style: DefaultStyle(),
		label: "Uploading",
		color: DefaultColors(),
	}
	// 获取终端宽度
	if tw, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && tw > 0 {
		pt.width = tw
	} else {
		// 不是终端或者获取失败, 输出原始内容
		pt.quiet = true
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
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.quiet = true
}

// Start 主要为开始计时
func (pt *Tracker) Start() {
	t := time.Now()
	pt.startAt = t
	if pt.quiet {
		return
	}
}

// Stop 输出最终统计信息
// 一般配合 defer pt.Stop()
func (pt *Tracker) Stop() {
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

	// 如果有失败的任务
	if f > 0 {
		summary += fmt.Sprintf(", %d failed", f)
	}

	// 如果设置了颜色输出
	if pt.color.Done != "" {
		summary = pt.color.Done + summary + ansiReset
	}

	// 换行打印统计信息
	_, _ = fmt.Fprintln(os.Stdout, "\n "+summary)

	// 打印失败任务列表
	for _, s := range pt.failedStrings {
		str := fmt.Sprintf(" failed: %s", s)
		if pt.color.Error != "" {
			str = pt.color.Error + str + ansiReset
		}
		_, _ = fmt.Fprintln(os.Stderr, str)
	}
}
