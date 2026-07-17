package progress

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/term"
)

const (
	// minRenderInterval 渲染节流间隔，约 20fps。
	// 人眼难以分辨更高刷新率，分片传输时避免每个 chunk 都重绘。
	minRenderInterval = 50 * time.Millisecond
	// widthRefreshInterval 终端宽度刷新间隔。
	// 终端宽度很少变化，无需每次渲染都执行 ioctl。
	widthRefreshInterval = time.Second
)

// AddTotal 增加总任务数
func (pt *Tracker) AddTotal(n int64) {
	pt.total.Add(n)
	if pt.quiet.Load() {
		return
	}
	if pt.totalSz.Load() == 0 {
		return
	}

	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.render(false)
}

// AddTotalDone 增加已完成任务数
// 不打印进度条时直接输出原始内容
func (pt *Tracker) AddTotalDone(n int64, msg string) {
	pt.done.Add(n)
	if pt.quiet.Load() {
		// 直接输出原始信息
		_, _ = fmt.Fprintln(os.Stdout, colorize(colorDone, fmt.Sprintf("Done: %s", msg)))
		return
	}

	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.render(false)
}

// AddTotalSize 增加总任务大小
func (pt *Tracker) AddTotalSize(sz int64) {
	pt.totalSz.Add(sz)
	if pt.quiet.Load() {
		return
	}
	if pt.total.Load() == 0 {
		return
	}
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.render(false)
}

// AddTotalSizeDone 增加已完成任务大小
func (pt *Tracker) AddTotalSizeDone(sz int64) {
	pt.doneSz.Add(sz)
	if pt.quiet.Load() {
		return
	}

	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.render(false)
}

// AddFailed 增加失败计数
// 失败也算任务完成，表示这个任务已经做过了，不会再重试
// 失败记录被汇总统计，在最后任务完成后输出
func (pt *Tracker) AddFailed(n int64, msg string) {
	pt.failed.Add(n)
	pt.done.Add(n)

	// 单次持锁：同时追加失败信息与渲染，避免重复加解锁
	pt.mu.Lock()
	defer pt.mu.Unlock()
	if msg != "" {
		pt.failedStrings = append(pt.failedStrings, msg)
	}
	if !pt.quiet.Load() {
		pt.render(false)
	}
}

// render 原地重绘单行进度条。调用方须持有 pt.mu。
// force=true 时跳过节流（用于终态/Stop，保证最后一帧显示真实数据）。
// 颜色完全由 pt.style 控制（buildBar 内部按段着色），此处只负责清行与定位。
func (pt *Tracker) render(force bool) {
	// 节流：非强制刷新且距上次渲染不足间隔则跳过
	now := time.Now()
	if !force {
		if now.UnixNano()-pt.lastRender < int64(minRenderInterval) {
			return
		}
	}
	pt.lastRender = now.UnixNano()

	// 终端宽度按间隔刷新，避免每帧都执行 ioctl
	if now.Sub(pt.widthCheckedAt) >= widthRefreshInterval {
		if tw, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && tw > 0 {
			pt.width = tw
		}
		pt.widthCheckedAt = now
	}

	// 清空当前行并打印进度条显示的字符串（合并为一次 write，减少 syscall）
	bar := pt.buildBar(pt.width)
	_, _ = fmt.Fprint(os.Stdout, clearLine+bar)
}
