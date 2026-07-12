package fmtutil

import (
	"bytes"
	"io"
	"testing"
)

func TestSetNewDisablesColorForBuffer(t *testing.T) {
	oldOutput, oldNoColor := GetOutput(), NoColor()
	t.Cleanup(func() { SetNew(oldOutput, oldNoColor) })
	var buf bytes.Buffer
	SetNew(&buf, false)
	PrintfRed("message %d", 1)
	if got := buf.String(); got != "message 1" {
		t.Fatalf("output = %q", got)
	}
	if !NoColor() {
		t.Fatal("non-terminal writer should disable colors")
	}
	if _, ok := GetOutput().(io.Writer); !ok {
		t.Fatal("output should be a writer")
	}
}
