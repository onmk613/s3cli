package progress

import (
	"strings"
	"testing"

	"s3cli/internal/fmtutil"
)

func TestBuildStyledBarBoundsAndStyle(t *testing.T) {
	style := &Style{LeftBracket: "[", RightBracket: "]", Filled: "=", Head: ">", Empty: "."}
	if got := buildStyledBar(style, 5, -1); got != "[.....]" {
		t.Fatalf("negative = %q", got)
	}
	if got := buildStyledBar(style, 5, 1); got != "[=====]" {
		t.Fatalf("complete = %q", got)
	}
	if got := buildStyledBar(style, 5, .5); got != "[==>..]" {
		t.Fatalf("half = %q", got)
	}
	if got := repeatToWidth("", 3); got != "   " {
		t.Fatalf("empty unit = %q", got)
	}
	// colorizeIf: 空颜色 / quiet / 全局关色时原样返回
	pt := &Tracker{}
	if got := pt.colorizeIf("", "text"); got != "text" {
		t.Fatalf("empty color passthrough = %q", got)
	}
	pt.SetQuiet()
	if got := pt.colorizeIf("\033[31m", "text"); got != "text" {
		t.Fatalf("quiet passthrough = %q", got)
	}
	// 强制开色后经 fmtutil.WrapANSI 包裹 (reset 码口径与 fmtutil 一致)
	pt2 := &Tracker{}
	fmtutil.SetColor(true)
	defer fmtutil.SetColorAuto()
	if got, want := pt2.colorizeIf("\033[31m", "text"), "\033[31mtext\033[0m"; got != want {
		t.Fatalf("wrap = %q, want %q", got, want)
	}
}

func TestTrackerBuildBarCapsProgress(t *testing.T) {
	pt := New()
	pt.SetLabel("test")
	pt.AddTotal(1)
	pt.AddTotalSize(10)
	pt.AddTotalDone(2, "")
	pt.AddTotalSizeDone(20)
	if got := pt.buildBar(120); !strings.Contains(got, "100%") {
		t.Fatalf("bar = %q", got)
	}
}

func TestStringWidthCJK(t *testing.T) {
	// 中文按显示列 2 计, 而非 UTF-8 字节数 3
	if got := stringWidth("中文"); got != 4 {
		t.Errorf("stringWidth(中文) = %d, want 4", got)
	}
	if got := stringWidth("abc"); got != 3 {
		t.Errorf("stringWidth(abc) = %d, want 3", got)
	}
	// ANSI 序列不占宽度 (复用 fmtutil.DisplayWidth 内的剥离逻辑)
	if got := stringWidth("\x1b[32m中\x1b[0m"); got != 2 {
		t.Errorf("stringWidth with ANSI+CJK = %d, want 2", got)
	}
}
