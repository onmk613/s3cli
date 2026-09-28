package action

import (
	"context"
	"math"
	"testing"
)

// TestValidateConcurrencyBounds 覆盖边界: <=0 表示"未设置"(合法, 由调用方填默认值),
// 1..MaxConcurrency 合法, 超出上界报错。
func TestValidateConcurrencyBounds(t *testing.T) {
	valid := []int{math.MinInt, -1, 0, 1, 2, defaultConcurrency, MaxConcurrency}
	for _, n := range valid {
		if err := validateConcurrency(n); err != nil {
			t.Errorf("validateConcurrency(%d) = %v, want nil", n, err)
		}
	}
	invalid := []int{MaxConcurrency + 1, 1 << 20, math.MaxInt32, math.MaxInt64}
	for _, n := range invalid {
		if err := validateConcurrency(n); err == nil {
			t.Errorf("validateConcurrency(%d) = nil, want error", n)
		}
	}
}

// TestValidatePartSizeBounds 覆盖分片大小边界 (0 = 用默认值)。
func TestValidatePartSizeBounds(t *testing.T) {
	for _, mb := range []int{math.MinInt, -1, 0, 1, 15, MaxPartSizeMB} {
		if err := validatePartSizeMB(mb); err != nil {
			t.Errorf("validatePartSizeMB(%d) = %v, want nil", mb, err)
		}
	}
	for _, mb := range []int{MaxPartSizeMB + 1, 5120, 1 << 20, math.MaxInt32} {
		if err := validatePartSizeMB(mb); err == nil {
			t.Errorf("validatePartSizeMB(%d) = nil, want error", mb)
		}
	}
}

// TestRunStreamRejectsHugeConcurrencyWithoutPanic 是回归测试。
//
// 修复前 RunStream 直接执行 `make(chan StreamJob, cfg.Concurrency*2)`:
// `--concurrency 9223372036854775807` 触发 "panic: makechan: size out of range",
// 进程带着 goroutine 栈回溯崩溃 (退出码 2); 稍小的值不 panic 但会申请等量
// 缓冲直接 OOM。现在必须在分配之前返回普通错误。
func TestRunStreamRejectsHugeConcurrencyWithoutPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("RunStream panicked instead of returning an error: %v", r)
		}
	}()

	for _, n := range []int{math.MaxInt64, math.MaxInt32, MaxConcurrency + 1} {
		err := RunStream(context.Background(), StreamConfig{
			Concurrency: n,
			Label:       "test",
			NoProgress:  true,
			Scan: func(context.Context, chan<- StreamJob) error {
				t.Error("Scan must not run when concurrency is invalid")
				return nil
			},
			Work: func(context.Context, StreamJob, func(int64)) error {
				t.Error("Work must not run when concurrency is invalid")
				return nil
			},
		})
		if err == nil {
			t.Fatalf("RunStream(concurrency=%d) = nil, want a validation error", n)
		}
	}
}

// TestEntryPointsRejectHugeConcurrencyWithoutPanic 覆盖各命令入口, 确保
// 没有任何一条路径能绕过校验走到分配处。
func TestEntryPointsRejectHugeConcurrencyWithoutPanic(t *testing.T) {
	server, requests := countRequests(t)
	a := &Action{S3: actionTestClient(t, server.URL, nil), Alias: "test", Ctx: context.Background()}

	huge := math.MaxInt64
	calls := map[string]func() error{
		"GetObject": func() error {
			return a.GetObject(GetOptions{Recursive: true, Concurrency: huge, NoProgress: true}, "bucket", "dir", t.TempDir())
		},
		"PutObject": func() error {
			return a.PutObject(PutOptions{Recursive: true, Concurrency: huge, NoProgress: true}, "bucket", "dir", t.TempDir(), false)
		},
		"CopyObjects": func() error {
			return a.CopyObjects(CopyOptions{Recursive: true, Concurrency: huge, NoProgress: true}, "bucket", "dir", "bucket", "out/")
		},
		"Mv": func() error {
			return a.Mv(CopyOptions{Recursive: true, Concurrency: huge, NoProgress: true}, "bucket", "dir", "bucket", "out/")
		},
		"Diff": func() error {
			return Diff(DiffOptions{Concurrency: huge})
		},
	}
	for name, fn := range calls {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("%s panicked instead of returning an error: %v", name, r)
				}
			}()
			if err := fn(); err == nil {
				t.Fatalf("%s(concurrency=%d) = nil, want a validation error", name, huge)
			}
		})
	}

	if n := requests.Load(); n != 0 {
		t.Fatalf("rejected calls still issued %d HTTP request(s); validation must happen before any I/O", n)
	}
}

// TestPartSizeLimitBlocksBeforeUpload 确保超大 --part-size 在上传前被拒。
func TestPartSizeLimitBlocksBeforeUpload(t *testing.T) {
	server, requests := countRequests(t)
	a := &Action{S3: actionTestClient(t, server.URL, nil), Alias: "test", Ctx: context.Background()}

	err := a.PutObject(PutOptions{PartSizeMB: 5120, NoProgress: true}, "bucket", "k", "-", false)
	if err == nil {
		t.Fatal("put with --part-size 5120 must be rejected")
	}
	if n := requests.Load(); n != 0 {
		t.Fatalf("rejected put still issued %d request(s)", n)
	}
}
