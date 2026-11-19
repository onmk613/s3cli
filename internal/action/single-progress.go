package action

import (
	"github.com/onmk613/s3cli/pkg/progress"
)

const (
	// singleProgressMinSize 是单文件下载/复制显示进度的最小大小。
	// 更小的传输通常在一秒内完成，进度行只会一闪而过，反而干扰输出。
	singleProgressMinSize = int64(8 << 20)

	// uploadProgressMinSize 是单文件上传显示进度的最小大小。
	// 小于分片阈值的文件走单次 PUT，没有分块回调；此时画一行停在 0%
	// 的进度只会误导用户，因此只对会走分片上传的大文件显示。
	uploadProgressMinSize = multipartThreshold
)

// newSingleFileTracker 创建单文件传输用的统计式进度跟踪器（无填充条）。
//
// 返回 nil 表示不显示进度：调用方指定了 --quiet，或 stdout 不是终端
// （重定向/管道场景只保留原有的最终结果行）。
func newSingleFileTracker(label string, noProgress bool) *progress.Tracker {
	if noProgress {
		return nil
	}
	pt := progress.NewSingleFile(label)
	if !pt.Enabled() {
		return nil
	}
	return pt
}

// singleFileReport 把进度跟踪器包装成 AddTotalSizeDone 回调。
// pt 为 nil 时返回 nil，便于直接传给下载/上传路径。
func singleFileReport(pt *progress.Tracker) func(int64) {
	if pt == nil {
		return nil
	}
	return func(n int64) {
		if n > 0 {
			pt.AddTotalSizeDone(n)
		}
	}
}
