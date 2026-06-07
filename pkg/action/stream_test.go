package action

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"

	myprint "s3cli/pkg/fmtutil"
)

func init() {
	myprint.NewFormat(io.Discard, false, true)
}

func TestRunStream_Basic(t *testing.T) {
	var processed atomic.Int64

	err := RunStream(context.Background(), StreamConfig{
		Concurrency: 4,
		Label:       "test",
		Scan: func(ctx context.Context, jobs chan<- StreamJob) error {
			for i := 0; i < 10; i++ {
				jobs <- StreamJob{Src: "src", Dst: "dst", Size: 100}
			}
			return nil
		},
		Work: func(ctx context.Context, job StreamJob) error {
			processed.Add(1)
			return nil
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if processed.Load() != 10 {
		t.Errorf("processed = %d, want 10", processed.Load())
	}
}

func TestRunStream_ScanError(t *testing.T) {
	scanErr := errors.New("scan failed")

	err := RunStream(context.Background(), StreamConfig{
		Concurrency: 2,
		Label:       "test",
		Scan: func(ctx context.Context, jobs chan<- StreamJob) error {
			return scanErr
		},
		Work: func(ctx context.Context, job StreamJob) error {
			return nil
		},
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, scanErr) {
		t.Errorf("got %v, want %v", err, scanErr)
	}
}

func TestRunStream_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	err := RunStream(ctx, StreamConfig{
		Concurrency: 2,
		Label:       "test",
		Scan: func(ctx context.Context, jobs chan<- StreamJob) error {
			jobs <- StreamJob{Src: "s", Dst: "d", Size: 1}
			return nil
		},
		Work: func(ctx context.Context, job StreamJob) error {
			return nil
		},
	})
	// 取消不应 panic
	_ = err
}

func TestRunStream_ZeroConcurrency(t *testing.T) {
	// 并发数为 0 应默认调整为 10
	err := RunStream(context.Background(), StreamConfig{
		Concurrency: 0,
		Label:       "test",
		Scan: func(ctx context.Context, jobs chan<- StreamJob) error {
			return nil
		},
		Work: func(ctx context.Context, job StreamJob) error {
			return nil
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
