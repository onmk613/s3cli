package action

import (
	"context"
	"fmt"
	"sync"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/progress"
)

// StreamJob 流式操作中的一个任务。
type StreamJob struct {
	Src  string // 源路径（本地或 S3）
	Dst  string // 目标路径
	Size int64  // 文件大小（字节）
}

// StreamConfig 描述一次流式操作（put/get/cp/mv）的参数。
type StreamConfig struct {
	Concurrency int    // 并发工作数
	ScrollMax   int    // 进度条滚动条数
	Label       string // 进度条标签（"put"/"get"/"cp"/"mv"）

	// Scan 扫描协程：向 jobs 通道写入任务。
	// 返回 error 表示扫描失败。
	Scan func(ctx context.Context, jobs chan<- StreamJob) error

	// Work 处理一个任务。返回 error 表示该任务失败（会记录到进度条）。
	Work func(ctx context.Context, job StreamJob) error
}

// RunStream 执行流式操作：扫描 → 并发处理 → 进度跟踪。
// ctx 取消会触发尽早退出。
func RunStream(ctx context.Context, cfg StreamConfig) error {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 10
	}

	pt := progress.NewProgressTracker(myprint.GetOutput(), cfg.ScrollMax, cfg.Label)
	pt.SetContextDone(ctx.Done())
	pt.Start()
	defer pt.Stop()

	jobs := make(chan StreamJob, cfg.Concurrency*2)
	scanErr := make(chan error, 1)

	// 扫描协程：在派发任务时累加 total，使进度条的分母随发现的任务增长，
	// 而 done 永远滞后于 total，避免 done≈total 时百分比/ETA 来回抖动。
	go func() {
		defer close(jobs)
		// 包一层 channel，扫描器每写入一个 job 就累加一次 total。
		relay := make(chan StreamJob, cfg.Concurrency*2)
		go func() {
			defer close(relay)
			if err := cfg.Scan(ctx, relay); err != nil {
				scanErr <- err
			}
		}()
		for j := range relay {
			pt.AddTotal(1, j.Size)
			select {
			case jobs <- j:
			case <-ctx.Done():
				return
			}
		}
	}()

	// 工作协程：只负责处理与累加 done。
	var wg sync.WaitGroup
	for i := 0; i < cfg.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				if err := cfg.Work(ctx, j); err != nil {
					pt.AddFailed()
					pt.AddDone(1, j.Size, fmt.Sprintf("✗ %s → %s", j.Src, j.Dst))
				} else {
					pt.AddDone(1, j.Size, fmt.Sprintf("✓ %s → %s (%s)", j.Src, j.Dst, myprint.FormatBytes(j.Size)))
				}
			}
		}()
	}
	wg.Wait()

	// 检查扫描错误
	select {
	case err := <-scanErr:
		return err
	default:
	}
	return nil
}
