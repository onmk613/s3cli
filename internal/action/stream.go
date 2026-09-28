// stream.go 实现流式传输框架 RunStream: 扫描 -> 并发处理 -> 进度跟踪,
// 统一支撑 put/get/cp/mv 的批量并发传输, 带可选的预统计 (Count) 与进度条.

package action

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"s3cli/internal/fmtutil"
	"s3cli/internal/progress"
	"sync"
	"sync/atomic"

	"s3cli/internal/api"
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
	Label       string // 进度条标签（"put"/"get"/"cp"/"mv"）
	NoProgress  bool   // 为 true 时不显示进度条（--quiet / 非终端场景）

	// Scan 扫描协程：向 jobs 通道写入任务。
	// 返回 error 表示扫描失败。
	Scan func(ctx context.Context, jobs chan<- StreamJob) error

	// Work 处理一个任务。返回 error 表示该任务失败（会记录到进度条）。
	// report 用于在分片传输过程中实时上报本次新增的字节数（增量）。
	// 对于无法获知分片进度的操作（如服务端 CopyObject），忽略 report 即可，
	// RunStream 会在任务成功后自动按 job.Size 对账补齐进度。
	Work func(ctx context.Context, job StreamJob, report func(n int64)) error

	// Count 可选的预统计协程：在独立 goroutine 中提前快速遍历数据源
	// （S3 用 ListObjectsV2Paginator，本地用 filepath.Walk），通过 add(n, size)
	// 增量上报发现的对象数和字节数，使进度条的 total/totalSize 尽早接近真实总量，
	// 而不必等扫描+传输边走边累加。
	//
	// 提供 Count 时，Scan 阶段不再累加 total/totalSize（由 Count 独占），
	// 避免重复计数；未提供时退回 Scan 边派发边累加的旧行为。
	// Count 失败仅退化为旧行为（进度分母随扫描增长），不影响实际传输。
	Count func(ctx context.Context, add func(n, size int64)) error
}

// RunStream 执行流式操作：扫描 → 并发处理 → 进度跟踪。
// ctx 取消会触发尽早退出。
func RunStream(ctx context.Context, cfg StreamConfig) error {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = defaultConcurrency
	}
	// 兜底校验: 各命令入口已校验, 这里是最后一道防线 —— 下面立刻会用
	// cfg.Concurrency*2 申请 channel 缓冲并起 N 个 worker。
	if err := validateConcurrency(cfg.Concurrency); err != nil {
		return err
	}

	pt := progress.New()
	if cfg.NoProgress {
		pt.SetQuiet()
	}
	pt.SetLabel(cfg.Label)
	pt.Start()
	defer pt.Stop()

	// 预统计协程：提供 Count 时，用独立 goroutine 提前遍历数据源累加 total/totalSize，
	// 让进度条分母尽早接近真实总量；此时扫描阶段不再重复累加。
	// countWg/cancelCount 保证 RunStream 返回前 Count 协程已结束:
	// 否则 Stop() 打印汇总后 Count 仍在渲染, 终端错乱且协程泄漏。
	// Count 失败时置 countFailed, 由 Scan 恢复累加 (与 StreamConfig.Count 注释约定一致);
	// 该兜底下分母可能轻微偏大 (Count 已加一部分), 仅影响失败路径的显示精度。
	var countWg sync.WaitGroup
	var countFailed atomic.Bool
	countTotals := cfg.Count != nil
	countCtx, cancelCount := context.WithCancel(ctx)
	defer cancelCount()
	if countTotals {
		countWg.Go(func() {
			if err := cfg.Count(countCtx, func(n, size int64) {
				if n != 0 {
					pt.AddTotal(n)
				}
				if size != 0 {
					pt.AddTotalSize(size)
				}
			}); err != nil && countCtx.Err() == nil {
				countFailed.Store(true)
			}
		})
	}

	jobs := make(chan StreamJob, cfg.Concurrency*2)
	scanErr := make(chan error, 1)

	// 扫描协程：在派发任务时累加 total，使进度条的分母随发现的任务增长，
	// 而 done 永远滞后于 total，避免 done≈total 时百分比/ETA 来回抖动。
	// 若已有 Count 协程负责累加，则此处只派发不累加，避免重复计数。
	//
	// scanWg 纳入外层转发协程、内层 Scan 协程以及取消时排空 relay 的 drain
	// 协程：RunStream 返回前统一等待扫描链路全部结束，杜绝返回后仍有后台
	// 扫描 IO/协程泄漏（如目录遍历继续 walk、分页请求仍 in-flight）。
	var scanWg sync.WaitGroup
	scanWg.Go(func() {
		defer close(jobs)
		// 包一层 channel，扫描器每写入一个 job 就累加一次 total。
		relay := make(chan StreamJob, cfg.Concurrency*2)
		scanWg.Go(func() {
			defer close(relay)
			if err := cfg.Scan(ctx, relay); err != nil {
				scanErr <- err
			}
		})
		for j := range relay {
			if !countTotals || countFailed.Load() {
				pt.AddTotal(1)
				pt.AddTotalSize(j.Size)
			}
			select {
			case jobs <- j:
			case <-ctx.Done():
				// 提前退出时排空 relay, 否则内部 Scan 协程会永远阻塞在写入上。
				// drain 协程纳入 scanWg, 由 RunStream 返回前统一等待。
				scanWg.Go(func() {
					for range relay {
					}
				})
				return
			}
		}
	})

	// 工作协程：只负责处理与累加 done。
	//
	// 失败既要逐个报给进度条（用户能看到具体是哪个文件），也要汇总后由
	// RunStream 返回 —— 否则 `put -r` / `get -r` / `cp -r` / `mv -r` 在部分
	// 文件失败时仍以退出码 0 结束，脚本无法感知数据没传全。
	var (
		failMu    sync.Mutex
		failCount int64
		firstErr  error
		attempted atomic.Int64
	)
	var wg sync.WaitGroup
	for i := 0; i < cfg.Concurrency; i++ {
		wg.Go(func() {
			for j := range jobs {
				// 已被取消（Ctrl+C）则停止处理剩余任务，
				// 不把中断导致的错误误记为"失败"。
				if ctx.Err() != nil {
					return
				}

				// reported 记录本任务已通过 report 累加到进度条的字节数，
				// 便于成功后对账补齐、失败后回退，保证进度条字节精确。
				// 用 atomic: 分片上传会从多个 worker goroutine 并发回调 report。
				var reported atomic.Int64
				report := func(n int64) {
					if n == 0 {
						return
					}
					reported.Add(n)
					pt.AddTotalSizeDone(n)
				}

				msg := fmt.Sprintf("%s → %s (%s)", j.Src, j.Dst, fmtutil.FormatBytes(j.Size))
				attempted.Add(1)
				if err := cfg.Work(ctx, j, report); err != nil {
					if ctx.Err() != nil {
						return
					}
					// 失败：回退本任务已上报的字节，避免失败文件虚增进度。
					if r := reported.Load(); r != 0 {
						pt.AddTotalSizeDone(-r)
					}
					pt.AddFailed(1, fmt.Sprintf("Failed %s: %s", msg, err))

					failMu.Lock()
					failCount++
					if firstErr == nil {
						firstErr = fmt.Errorf("%s: %w", msg, err)
					}
					failMu.Unlock()
				} else {
					// 成功：对账，把进度精确补齐到 job.Size。
					// 适配无分片进度的操作（report 未被调用，reported==0）。
					if diff := j.Size - reported.Load(); diff != 0 {
						pt.AddTotalSizeDone(diff)
					}
					pt.AddTotalDone(1, msg)
				}
			}
		})
	}
	wg.Wait()

	// 传输已结束: 取消并等待 Count 协程退出, 保证 Stop() 打印汇总后
	// 不再有协程向终端渲染进度条。
	cancelCount()
	countWg.Wait()

	// 等待扫描协程 (外层转发 + 内层 Scan + 取消时排空 relay 的 drain) 全部结束,
	// 再检查扫描错误并返回, 杜绝 RunStream 返回后仍有后台 IO/协程泄漏。
	scanWg.Wait()

	// 用户取消必须显式上抛 ctx 错误。
	//
	// 各 worker 在 ctx 取消时是"提前 return"而不是记 failCount (见上面的
	// `if ctx.Err() != nil { return }`), 扫描器同理; 若列举早已结束 (典型场景:
	// 列举很快、传输很久), scanErr 也是空的 —— 于是本函数会返回 nil, 上层
	// cmd/root.go 便以退出码 0 结束。Ctrl+C 被报告成"传输成功", 脚本据此认为
	// 数据已完整同步。这里补上唯一的权威判据: ctx 已取消就是未完成。
	// 放在 scanErr 之前: 取消时的扫描错误通常是取消的副作用, 报 130 更准确。
	if err := ctx.Err(); err != nil {
		return err
	}

	// 检查扫描错误
	select {
	case err := <-scanErr:
		return err
	default:
	}

	// 任务失败必须上抛: 进度条已经把每个失败对象打印给用户, 这里返回一个
	// 带计数的汇总错误 (firstErr 保留错误链, 供退出码映射识别 404/403)。
	// 用户主动取消时不算失败 —— 各 worker 已提前返回, 不进入 failCount。
	if failCount > 0 {
		return fmt.Errorf("%d of %d transfer(s) failed; first: %w",
			failCount, attempted.Load(), firstErr)
	}
	return nil
}

// countS3Prefix 遍历 bucket 下 prefix 的所有对象，通过 add 增量上报对象数与字节数，
// 用作 StreamConfig.Count 的 S3 端实现（get/cp/mv 的预统计）。
// skipDirMarker=true 时跳过 0 字节的目录占位对象（与 get 的扫描逻辑保持一致）。
func (c *Action) countS3Prefix(ctx context.Context, bucket, prefix string, skipDirMarker bool, add func(n, size int64)) error {
	return c.forEachObject(ctx, bucket, prefix, func(obj api.ObjectInfo) error {
		size := obj.Size
		if skipDirMarker && size == 0 {
			key := obj.Key
			if len(key) > 0 && key[len(key)-1] == '/' {
				return nil
			}
		}
		add(1, size)
		return nil
	})
}

// countLocalDir 遍历本地目录 root 下的所有普通文件，通过 add 增量上报文件数与字节数，
// 用作 StreamConfig.Count 的本地端实现（put 的预统计）。
func countLocalDir(root string, add func(n, size int64)) error {
	return filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		add(1, info.Size())
		return nil
	})
}
