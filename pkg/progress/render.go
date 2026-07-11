package progress

import (
	"fmt"

	"golang.org/x/term"
)

// AddTotal 增加总任务数
func (pt *Tracker) AddTotal(n int64) {
	pt.total.Add(n)

	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.render()
}

// AddTotalDone 增加已完成任务数
func (pt *Tracker) AddTotalDone(n int64) {
	pt.done.Add(n)

	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.render()
}

// AddTotalSize 增加总任务大小
func (pt *Tracker) AddTotalSize(sz int64) {
	pt.totalSz.Add(sz)

	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.render()
}

// AddTotalSizeDone 增加已完成任务大小
func (pt *Tracker) AddTotalSizeDone(sz int64) {
	pt.doneSz.Add(sz)

	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.render()
}

// AddFailed 增加失败计数
func (pt *Tracker) AddFailed(msg string) {
	pt.failed.Add(1)
	if msg != "" {
		pt.mu.Lock()
		pt.failedStrings = append(pt.failedStrings, msg)
		pt.mu.Unlock()
	}
}

// render 原地重绘单行进度条。调用方须持有 pt.mu。
// 颜色完全由 pt.style 控制（buildBar 内部按段着色），此处只负责清行与定位。
func (pt *Tracker) render() {
	wd := pt.width
	if tw, _, err := term.GetSize(pt.fd); err == nil && tw > 0 {
		wd = tw
		pt.width = wd
	}

	// 清空当前行并打印进度条显示的字符串
	_, _ = fmt.Fprint(pt.output, clearLine+pt.buildBar(wd))
}
