package progress

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// TestBuildStatsLineSingleFile 断言单文件统计行的字段顺序与格式:
// label | 已传/总量 | 速率 | 百分比 | 耗时 | 剩余。
func TestBuildStatsLineSingleFile(t *testing.T) {
	pt := &Tracker{label: "put"}
	pt.totalSz.Store(1000)
	pt.doneSz.Store(130)

	got := pt.buildStatsLine("13.27MB/s", "4m02s", "26m01s")
	want := "put | 130B/1000B | 13.27MB/s |  13% | 4m02s | 26m01s"
	if got != want {
		t.Fatalf("stats line = %q, want %q", got, want)
	}
}

// TestBuildStatsLineUnknownTotal 总量未知时 (如 stdin 上传) 不显示
// "x/0B" 与百分比，只保留已传字节、速率与耗时。
func TestBuildStatsLineUnknownTotal(t *testing.T) {
	pt := &Tracker{label: "put"}
	pt.doneSz.Store(313)

	got := pt.buildStatsLine("13.27MB/s", "4m02s", "")
	want := "put | 313B | 13.27MB/s | 4m02s"
	if got != want {
		t.Fatalf("unknown-total line = %q, want %q", got, want)
	}
	if strings.Contains(got, "%") {
		t.Fatalf("unknown-total line must not show a percentage: %q", got)
	}
}

// TestBuildBarStatsOnlyHasNoFiller 断言统计模式不输出任何填充物/括号，
// 且文本行的字段顺序与用户要求一致。
func TestBuildBarStatsOnlyHasNoFiller(t *testing.T) {
	pt := New()
	pt.statsOnly.Store(true)
	pt.quiet.Store(false)
	pt.label = "get"
	pt.totalSz.Store(1000)
	pt.doneSz.Store(500)
	pt.startAt = time.Now()

	got := pt.buildBar(120)
	if strings.ContainsAny(got, "▓█░[]") {
		t.Fatalf("stats-only line must not contain bar filler: %q", got)
	}
	if !strings.Contains(got, "get | 500B/1000B | ") {
		t.Fatalf("stats-only line = %q", got)
	}
	if !strings.Contains(got, "50%") {
		t.Fatalf("stats-only line missing percentage: %q", got)
	}
}

// TestStopStatsOnlySingleFileEndsLineWithoutSummary 单文件模式结束后只换行，
// 不再输出 "0/0 total" 这类无意义的批量汇总。
func TestStopStatsOnlySingleFileEndsLineWithoutSummary(t *testing.T) {
	pt := New()
	pt.statsOnly.Store(true)
	pt.quiet.Store(false)
	pt.width = 80
	pt.label = "put"

	var buf bytes.Buffer
	pt.SetOutput(&buf)

	pt.Start()
	pt.AddTotalSize(100)
	pt.AddTotalSizeDone(100)
	pt.Stop()

	out := buf.String()
	if !strings.Contains(out, "100B/100B") {
		t.Fatalf("final stats frame missing: %q", out)
	}
	if strings.Contains(out, "total (") {
		t.Fatalf("single-file mode must not print a batch summary: %q", out)
	}
	if !strings.HasSuffix(out, "\n") {
		t.Fatalf("final stats line must be terminated by a newline: %q", out)
	}
}

// TestStopStatsOnlyQuietIsSilent 小文件/非终端场景把 tracker 静默后，
// Stop 不得输出任何内容（结果行仍由调用方打印）。
func TestStopStatsOnlyQuietIsSilent(t *testing.T) {
	pt := New()
	pt.statsOnly.Store(true)
	pt.SetQuiet()
	pt.label = "get"

	var buf bytes.Buffer
	pt.SetOutput(&buf)

	pt.Start()
	pt.Stop()

	if buf.Len() != 0 {
		t.Fatalf("quiet single-file progress wrote output: %q", buf.String())
	}
}

// TestStopWithoutStartIsSilent 在拿到响应头前就失败（未 Start）的场景，
// Stop 不能画出 time.Since(zero) 的垃圾耗时帧。
func TestStopWithoutStartIsSilent(t *testing.T) {
	pt := New()
	pt.statsOnly.Store(true)
	pt.quiet.Store(false)
	pt.width = 80
	pt.label = "get"

	var buf bytes.Buffer
	pt.SetOutput(&buf)

	pt.Stop()
	if buf.Len() != 0 {
		t.Fatalf("Stop without Start wrote output: %q", buf.String())
	}
}
