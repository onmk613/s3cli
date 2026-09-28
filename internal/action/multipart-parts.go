// multipart-parts.go 实现分片上传的并行分片调度。
//
// 原实现是「读一片 -> 传一片」的串行循环: 单个大文件只能跑满一条连接, 是相对
// mc / aws s3 cp 最明显的性能差距。RunStream 的并发是跨文件的, 对单文件无效。
//
// 这里把读取与上传解耦: 一个生产者顺序读分片 (Reader 不可并行读), N 个 worker
// 并发上传。内存上界 = multipartConcurrency × partSize (默认 4 × 15MiB = 60MiB)。

package action

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"

	"s3cli/internal/api"
)

// multipartConcurrency 是单个对象内部并发上传的分片数。
//
// 取值权衡: 分片上传是网络受限, 4 条并发连接已能跑满大多数链路; 再高只会成倍
// 放大内存占用 (每片一个 partSize 缓冲区), 而这些内存往往被浪费在等待上。
const multipartConcurrency = 4

// partJob 是一个待上传的分片。
type partJob struct {
	number int
	data   []byte
}

// partOutcome 是一个分片的上传结果。
type partOutcome struct {
	part api.CompletedPart
	n    int
	err  error
}

// runPartPipeline 启动「生产者读 -> N worker 上传 -> 收集结果」的流水线,
// 返回按分片号升序排列的已完成分片。
//
// readPart 由生产者串行调用, 返回 (partNumber, data, err);
// 返回 err == io.EOF 表示读尽。每次返回的切片必须是新分配的 —— 它会被交给
// worker 并发上传, 生产者不得复用。
func runPartPipeline(
	ctx context.Context,
	readPart func() (number int, data []byte, err error),
	uploadPart func(ctx context.Context, number int, data []byte) (api.CompletedPart, error),
	onPartDone func(n int64),
) ([]api.CompletedPart, error) {
	workers := multipartConcurrency
	jobs := make(chan partJob, workers)
	results := make(chan partOutcome, workers)

	// 派生可取消的 ctx: 任一分片失败即让其余 worker 尽早退出, 不再空传数据。
	pipeCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for job := range jobs {
				if pipeCtx.Err() != nil {
					return
				}
				part, err := uploadPart(pipeCtx, job.number, job.data)
				out := partOutcome{part: part, n: len(job.data), err: err}
				select {
				case results <- out:
				case <-ctx.Done():
					return
				}
				if err != nil {
					return // 首个失败者让其余 worker 自然收敛
				}
			}
		})
	}

	// 生产者: 顺序读取, 把分片交给 worker。读取错误记录在 readErr,
	// 由下方收集方在 wg.Wait() 之后读取。
	//
	// readErr 的写 -> 读同步依赖「生产者也加入 wg」: worker 在取消时存在不经
	// jobs 关闭的提前退出路径 (pipeCtx.Err() 与外层 ctx.Done), 若只等 worker,
	// wg.Wait() 可能在生产者写 readErr 之前返回, 构成数据竞争。
	var readErr error
	wg.Go(func() {
		defer close(jobs)
		for {
			if pipeCtx.Err() != nil {
				return
			}
			number, data, err := readPart()
			if err != nil {
				readErr = err
				return
			}
			select {
			case jobs <- partJob{number: number, data: data}:
			case <-pipeCtx.Done():
				return
			}
		}
	})

	// 收集: 必须等 worker 与生产者全部退出后再关 results, 否则会向已关闭通道发送。
	go func() {
		wg.Wait()
		close(results)
	}()

	var (
		parts    []api.CompletedPart
		firstErr error
	)
	for out := range results {
		if out.err != nil {
			if firstErr == nil {
				firstErr = out.err
				cancel()
			}
			continue
		}
		parts = append(parts, out.part)
		if onPartDone != nil {
			onPartDone(int64(out.n))
		}
	}

	if firstErr != nil {
		return nil, firstErr
	}
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return nil, readErr
	}

	sort.Slice(parts, func(i, j int) bool { return parts[i].PartNumber < parts[j].PartNumber })
	return parts, nil
}

// abortOnError 在 err != nil 时中止服务端分片上传 (用 WithoutCancel:
// 取消场景下 ctx 已失效, 直接传它会连 Abort 一起取消, 服务端残留分片)。
func (c *Action) abortOnError(err *error, ctx context.Context, bucket, key, uploadID string) {
	if *err == nil {
		return
	}
	if abortErr := c.S3.AbortMultipartUpload(context.WithoutCancel(ctx), bucket, key, uploadID); abortErr != nil {
		// 清理失败必须让用户看到: 服务端会一直保存这些分片直到生命周期清理。
		fmt.Printf("warning: abort multipart upload %s: %v\n", uploadID, abortErr)
	}
}
