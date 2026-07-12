package progress

import (
	"fmt"
	"os"

	"golang.org/x/term"
)

// AddTotal 增加总任务数
func (pt *Tracker) AddTotal(n int64) {
	pt.total.Add(n)
	if pt.quiet {
		return
	}
	if pt.totalSz.Load() == 0 {
		return
	}

	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.render()
}

// AddTotalDone 增加已完成任务数
// 不打印进度条时直接输出原始内容
func (pt *Tracker) AddTotalDone(n int64, msg string) {
	pt.done.Add(n)
	if pt.quiet {
		// 直接输出原始信息
		_, _ = fmt.Fprintln(os.Stdout, colorize(colorDone, fmt.Sprintf("Done: %s", msg)))
		return
	}

	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.render()
}

// AddTotalSize 增加总任务大小
func (pt *Tracker) AddTotalSize(sz int64) {
	pt.totalSz.Add(sz)
	if pt.quiet {
		return
	}
	if pt.total.Load() == 0 {
		return
	}
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.render()
}

// AddTotalSizeDone 增加已完成任务大小
func (pt *Tracker) AddTotalSizeDone(sz int64) {
	pt.doneSz.Add(sz)
	if pt.quiet {
		return
	}

	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.render()
}

// AddFailed 增加失败计数
// 失败也算任务完成，表示这个任务已经做过了，不会再重试
// 失败记录被汇总统计，在最后任务完成后输出
func (pt *Tracker) AddFailed(n int64, msg string) {
	pt.failed.Add(n)
	pt.done.Add(n)
	if msg != "" {
		pt.mu.Lock()
		pt.failedStrings = append(pt.failedStrings, msg)
		pt.mu.Unlock()
	}
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.render()
}

// render 原地重绘单行进度条。调用方须持有 pt.mu。
// 颜色完全由 pt.style 控制（buildBar 内部按段着色），此处只负责清行与定位。
func (pt *Tracker) render() {
	wd := pt.width
	if tw, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && tw > 0 {
		wd = tw
		pt.width = wd
	}
	// 清空当前行并打印进度条显示的字符串
	bar := pt.buildBar(wd)
	_, _ = fmt.Fprint(os.Stdout, clearLine)
	_, _ = fmt.Fprint(os.Stdout, bar)
}
