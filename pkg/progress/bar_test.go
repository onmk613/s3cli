package progress

import (
	"strings"
	"testing"
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
	// 无颜色：原样返回
	if got := colorize("", "text"); got != "text" {
		t.Fatalf("no-color passthrough = %q", got)
	}
	// 有颜色：包裹 ANSI 并保留原文
	if got := colorize("red", "text"); !strings.Contains(got, "text") || !strings.Contains(got, ansiReset) {
		t.Fatalf("colorize = %q", got)
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
