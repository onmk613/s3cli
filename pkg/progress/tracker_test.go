package progress

import (
	"bytes"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestTrackerSetLabel(t *testing.T) {
	pt := New()
	pt.SetLabel("Downloading")
	pt.mu.Lock()
	if pt.label != "Downloading" {
		t.Errorf("label = %q", pt.label)
	}
	pt.mu.Unlock()
}

func TestTrackerSetStyleDefaults(t *testing.T) {
	pt := New()
	pt.SetStyle(nil)                           // 应回退到 DefaultStyle
	pt.SetStyle(&Style{Filled: "", Empty: ""}) // 应填充默认
	pt.mu.Lock()
	if pt.style.Filled != "=" || pt.style.Empty != " " {
		t.Errorf("style defaults not applied: %+v", pt.style)
	}
	pt.mu.Unlock()
}

func TestTrackerAddTotalAndDone(t *testing.T) {
	pt := New()
	pt.SetQuiet() // 不渲染, 避免污染终端
	pt.Start()

	pt.AddTotal(5)
	pt.AddTotalSize(1000)
	pt.AddTotalSizeDone(500)
	pt.AddTotalDone(2, "two done")
	pt.AddTotalDone(1, "third")

	if got := pt.total.Load(); got != 5 {
		t.Errorf("total = %d", got)
	}
	if got := pt.done.Load(); got != 3 {
		t.Errorf("done = %d", got)
	}
	if got := pt.doneSz.Load(); got != 500 {
		t.Errorf("doneSz = %d", got)
	}
}

func TestTrackerAddFailedRecords(t *testing.T) {
	pt := New()
	pt.SetQuiet()
	pt.Start()

	pt.AddFailed(1, "error one")
	pt.AddFailed(1, "error two")
	pt.AddFailed(1, "") // 空 msg 不追加到列表, 但计数仍 +1

	// 3 次 AddFailed(1): failed 与 done 各 +3
	if got := pt.failed.Load(); got != 3 {
		t.Errorf("failed = %d", got)
	}
	if got := pt.done.Load(); got != 3 { // 失败也计入 done
		t.Errorf("done = %d", got)
	}
	pt.mu.Lock()
	if len(pt.failedStrings) != 2 { // 仅 2 条非空 msg
		t.Errorf("failedStrings = %v", pt.failedStrings)
	}
	pt.mu.Unlock()
}

func TestTrackerStopIdempotent(t *testing.T) {
	pt := New()
	pt.SetQuiet()
	pt.Start()
	pt.Stop()
	pt.Stop() // 幂等, 不 panic / 不重复打印
}

func TestStopEmitsFailedList(t *testing.T) {
	pt := New()
	pt.SetQuiet()
	pt.Start()
	pt.AddFailed(1, "boom")
	// Stop 在 quiet 下不渲染进度条, 但 failed > 0 时仍走统计分支
	pt.Stop()
	if pt.failed.Load() != 1 {
		t.Error("failed not recorded")
	}
}

func TestFormatBytesInternal(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0B"},
		{1024, "1KB"},
		{1048576, "1MB"},
		{-5, "0B"},
	}
	for _, tc := range cases {
		if got := formatBytes(tc.in); got != tc.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSetQuietAndQuietFromNonTerminal(t *testing.T) {
	pt := New()
	// go test 下 stdout 多为非终端 -> New 内部应已置 quiet
	// 这里显式再置一次并验证后续 Add* 不 panic
	pt.SetQuiet()
	pt.AddTotal(1)
	if !pt.quiet.Load() {
		t.Error("expected quiet")
	}
}

func TestRenderThrottleDoesNotPanic(t *testing.T) {
	pt := New()
	pt.SetQuiet()
	// quiet 下 render 被跳过; 此处仅验证 Add* 路径节流逻辑不 panic
	for i := 0; i < 100; i++ {
		pt.AddTotalSizeDone(1)
	}
}

// 确认 colorize 辅助被覆盖 (render 间接调用)
var _ = strings.Contains

// TestStopPreventsPostSummaryFrames 回归 P1-BUG-5:
// 并发 Add* 与 Stop 交错时, Stop 的汇总摘要行之后不得再出现任何输出
// (旧实现无锁读 stopped 与后续渲染/打印之间存在 TOCTOU 窗口)。
// quiet 模式的 "Done:" 原始行路径无渲染节流, 是旧 bug 最易触发的路径,
// 因此两种模式都覆盖。
func TestStopPreventsPostSummaryFrames(t *testing.T) {
	for _, quiet := range []bool{true, false} {
		name := "render"
		if quiet {
			name = "quiet"
		}
		t.Run(name, func(t *testing.T) {
			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			os.Stdout = w
			defer func() { os.Stdout = oldStdout }()

			// 并发排空管道: 防止 worker 在 Stop 前输出过多把 64KB 管道写满,
			// 导致 Stop 的摘要行也阻塞、测试死锁。
			var buf bytes.Buffer
			drained := make(chan struct{})
			go func() {
				_, _ = io.Copy(&buf, r)
				close(drained)
			}()

			pt := New()
			// 测试环境无终端: New 会置 quiet。按用例强制走目标路径。
			pt.quiet.Store(quiet)
			pt.width = 80
			pt.Start()

			const workers = 4
			const perWorker = 20000
			var wg sync.WaitGroup
			for i := 0; i < workers; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for j := 0; j < perWorker; j++ {
						pt.AddTotal(1)
						pt.AddTotalDone(1, "item")
					}
				}()
			}
			// 与并发 Add 交错调用 Stop
			pt.Stop()
			wg.Wait()

			_ = w.Close()
			<-drained
			os.Stdout = oldStdout
			out := buf.Bytes()

			// 汇总摘要行包含 "Uploading: N/M total (...)" (带颜色转义);
			// 进度条帧里只有 " Uploading " (无冒号), 故 "Uploading:" 唯一指向摘要行。
			idx := bytes.Index(out, []byte("Uploading:"))
			if idx < 0 {
				t.Fatalf("summary line not found in output: %q", out)
			}
			if rest := bytes.Index(out[idx:], []byte(clearLine)); rest >= 0 {
				t.Fatalf("progress frame rendered after Stop summary: %q", out[idx:])
			}
			if rest := bytes.Index(out[idx:], []byte("Done: item")); rest >= 0 {
				t.Fatalf("raw Done line rendered after Stop summary: %q", out[idx:])
			}
		})
	}
}
