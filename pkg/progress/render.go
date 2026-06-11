package progress

import (
	"fmt"

	"golang.org/x/term"
)

// AddTotal 增加总任务数和总字节数
func (pt *ProgressTracker) AddTotal(n int64, sz int64) {
	pt.total.Add(n)
	pt.totalSz.Add(sz)

	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.render()
}

// AddDone 增加已完成任务数
func (pt *ProgressTracker) AddDone(n int64, sz int64) {
	pt.done.Add(n)
	pt.doneSz.Add(sz)

	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.render()
}

// AddFailed 增加失败计数
func (pt *ProgressTracker) AddFailed(msg string) {
	pt.failed.Add(1)
	if msg != "" {
		pt.mu.Lock()
		pt.failedStrings = append(pt.failedStrings, msg)
		pt.mu.Unlock()
	}
}

// render 原地重绘单行进度条。调用方须持有 pt.mu。
// 颜色完全由 pt.style 控制（buildBar 内部按段着色），此处只负责清行与定位。
func (pt *ProgressTracker) render() {
	wd := pt.width
	if tw, _, err := term.GetSize(pt.fd); err == nil && tw > 0 {
		wd = tw
		pt.width = wd
	}
	// \r 回行首，\033[K 清到行尾，再写进度条。
	fmt.Fprint(pt.output, "\r\033[K"+pt.buildBar(wd))
}
