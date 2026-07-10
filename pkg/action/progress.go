package action

import "s3cli/pkg/progress"

// progressReporter 抽象流式操作需要的进度上报能力。
//
// 设计意图：是否显示进度条由 action 层的参数（如 StreamConfig.NoProgress /
// MirrorOptions.NoProgress）控制，而不侵入 progress 包本身。progress 包只关心
// "如何渲染进度"，"要不要渲染"由调用方决定。
//
// *progress.ProgressTracker 天然实现本接口；关闭进度条时用 nopProgress 兜底，
// 使 RunStream / copyOne 等无需到处写 if-else 判断。
type progressReporter interface {
	SetLabel(label string)
	Start()
	Stop()
	AddTotal(n int64)
	AddTotalDone(n int64)
	AddTotalSize(sz int64)
	AddTotalSizeDone(sz int64)
	AddFailed(msg string)
}

// newProgress 根据 noProgress 返回真实进度条或空实现。
// noProgress=true（如 --quiet 或非终端环境）时返回 nopProgress，全部方法为空操作。
func newProgress(noProgress bool) progressReporter {
	if noProgress {
		return nopProgress{}
	}
	return progress.New()
}

// nopProgress 是 progressReporter 的空实现：吞掉所有进度上报，不产生任何输出。
type nopProgress struct{}

func (nopProgress) SetLabel(string)        {}
func (nopProgress) Start()                 {}
func (nopProgress) Stop()                  {}
func (nopProgress) AddTotal(int64)         {}
func (nopProgress) AddTotalDone(int64)     {}
func (nopProgress) AddTotalSize(int64)     {}
func (nopProgress) AddTotalSizeDone(int64) {}
func (nopProgress) AddFailed(string)       {}
