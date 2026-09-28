package fmtutil

import "testing"

func TestWrapANSI(t *testing.T) {
	if got := WrapANSI("", "text"); got != "text" {
		t.Fatalf("empty code passthrough = %q", got)
	}
	if got, want := WrapANSI("\033[31m", "text"), "\033[31mtext"+resetCode; got != want {
		t.Fatalf("wrap = %q, want %q", got, want)
	}
}
