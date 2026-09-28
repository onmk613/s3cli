// multipart-parts_test.go 锁定 runPartPipeline 的错误传播与并发语义。
//
// 重点: 生产者读错误 readErr 的写 -> 读同步。历史上 worker 在取消时存在
// 不经 jobs 关闭的提前退出路径, wg.Wait() 可能先于生产者写 readErr 返回,
// 构成数据竞争 (go test -race 可检出); 修复方式是把生产者纳入同一 WaitGroup。
package action

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"s3cli/internal/api"
)

// TestRunPartPipelineReadErrAfterWorkersExited 验证: worker 因外层取消提前
// 退出后, 生产者稍后才返回的读错误仍能被可靠地观测并上抛, 而不是在
// close(results) 之后才发生写入 (数据竞争)。
func TestRunPartPipelineReadErrAfterWorkersExited(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sentinel := errors.New("simulated slow-read failure")
	reads := atomic.Int32{}

	parts, err := runPartPipeline(ctx,
		func() (int, []byte, error) {
			if reads.Add(1) == 1 {
				// 第一片正常产出, 让 worker 进入工作状态。
				return 1, make([]byte, 16), nil
			}
			// 模拟慢速 IO: 期间外层 ctx 已被取消、worker 已全部退出,
			// 生产者稍后才带着错误返回 —— 这是竞争窗口本身。
			time.Sleep(20 * time.Millisecond)
			return 0, nil, sentinel
		},
		func(ctx context.Context, number int, data []byte) (api.CompletedPart, error) {
			return api.CompletedPart{PartNumber: number, ETag: "etag"}, nil
		},
		func(n int64) {
			// 首片完成后立即取消: worker 将经由 ctx.Done 路径提前退出,
			// 不再消费 jobs, 生产者只能靠自己的错误路径收敛。
			cancel()
		},
	)

	if !errors.Is(err, sentinel) {
		t.Fatalf("runPartPipeline error = %v, want simulated slow-read failure", err)
	}
	if parts != nil {
		t.Fatalf("runPartPipeline parts = %v, want nil on read error", parts)
	}
}

// TestRunPartPipelineUploadFailurePropagates 验证任一分片上传失败时,
// 流水线收敛并返回首个错误, 而不是死锁或静默丢错。
func TestRunPartPipelineUploadFailurePropagates(t *testing.T) {
	ctx := context.Background()
	next := 0
	parts, err := runPartPipeline(ctx,
		func() (int, []byte, error) {
			next++
			if next > 5 {
				return 0, nil, io.EOF
			}
			return next, make([]byte, 8), nil
		},
		func(ctx context.Context, number int, data []byte) (api.CompletedPart, error) {
			if number == 3 {
				return api.CompletedPart{}, errors.New("upload part 3 failed")
			}
			return api.CompletedPart{PartNumber: number, ETag: "e"}, nil
		},
		nil,
	)
	if err == nil {
		t.Fatalf("runPartPipeline error = nil, want upload failure")
	}
	if parts != nil {
		t.Fatalf("runPartPipeline parts = %v, want nil on failure", parts)
	}
}
