package action

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

// TestRunStreamReturnsCanceledWhenListingAlreadyFinished 是回归测试。
//
// 修复前: 各 worker 在 ctx 取消时是"提前 return"而非记 failCount (正确, 取消
// 不该被当成传输失败), 扫描器同理。当列举早已结束 (典型场景: 列举很快、传输
// 很久) scanErr 也是空的, 于是 RunStream 返回 nil —— 上层 cmd/root.go 以退出码
// 0 结束, Ctrl+C 被报告成"传输成功", 脚本会据此认为数据已完整同步。
// 实测: 对慢速服务端 `get -r` 中途发 SIGINT, 修复前退出 0, 修复后 130。
//
// 本用例不依赖 sleep 或真实信号: Scan 一次性把所有 job 写入缓冲 channel 后即
// 返回 (模拟"列举瞬间完成"), Work 阻塞到 ctx 取消才返回。
func TestRunStreamReturnsCanceledWhenListingAlreadyFinished(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var once sync.Once
	workStarted := make(chan struct{})
	go func() {
		<-workStarted
		cancel()
	}()

	err := RunStream(ctx, StreamConfig{
		Concurrency: 1,
		NoProgress:  true,
		Scan: func(_ context.Context, jobs chan<- StreamJob) error {
			// 缓冲足够, 两个 job 立即写入后 Scan 返回 => 列举先于传输结束。
			jobs <- StreamJob{Src: "a", Dst: "b", Size: 10}
			jobs <- StreamJob{Src: "c", Dst: "d", Size: 10}
			return nil
		},
		Work: func(ctx context.Context, _ StreamJob, _ func(int64)) error {
			once.Do(func() { close(workStarted) })
			<-ctx.Done() // 卡住直到被取消
			return ctx.Err()
		},
	})

	if err == nil {
		t.Fatal("RunStream returned nil after cancellation; Ctrl+C would exit 0 instead of 130")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	// 退出码 130 的实际判据是 action.IsCanceled, 一并锁定。
	if !IsCanceled(err) {
		t.Fatalf("IsCanceled(%v) = false; the caller would not map this to exit code 130", err)
	}
}

// TestRunStreamCompletesNormallyWithoutCancel 对照组: 没有取消时必须返回 nil,
// 确保上面的修复不是"一律报取消"。
func TestRunStreamCompletesNormallyWithoutCancel(t *testing.T) {
	var done atomic.Int64
	err := RunStream(context.Background(), StreamConfig{
		Concurrency: 2,
		NoProgress:  true,
		Scan: func(_ context.Context, jobs chan<- StreamJob) error {
			for i := 0; i < 5; i++ {
				jobs <- StreamJob{Src: "s", Dst: "d", Size: 1}
			}
			return nil
		},
		Work: func(context.Context, StreamJob, func(int64)) error {
			done.Add(1)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("RunStream = %v, want nil", err)
	}
	if got := done.Load(); got != 5 {
		t.Fatalf("processed %d jobs, want 5", got)
	}
}

// TestRunStreamCancelBeatsScanError 取消时若扫描也报了错, 应返回 ctx 错误:
// 那类错误通常是取消的副作用, 报 130 (静默退出) 比打印一个衍生错误更准确。
func TestRunStreamCancelBeatsScanError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var once sync.Once
	workStarted := make(chan struct{})
	go func() {
		<-workStarted
		cancel()
	}()

	err := RunStream(ctx, StreamConfig{
		Concurrency: 1,
		NoProgress:  true,
		Scan: func(ctx context.Context, jobs chan<- StreamJob) error {
			jobs <- StreamJob{Src: "a", Dst: "b", Size: 1}
			<-ctx.Done()
			return errors.New("listing aborted because of cancellation")
		},
		Work: func(ctx context.Context, _ StreamJob, _ func(int64)) error {
			once.Do(func() { close(workStarted) })
			<-ctx.Done()
			return ctx.Err()
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled to take precedence over the scan error", err)
	}
}
