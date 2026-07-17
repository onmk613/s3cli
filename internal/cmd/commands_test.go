package cmd

import (
	"context"
	"testing"

	"s3cli/pkg/s3api"
)

func TestTransferCommandsExposeExpectedFlags(t *testing.T) {
	for _, tc := range []struct {
		name  string
		flags []string
		check func() []string
	}{
		{"get", []string{"recursive", "concurrency", "part-size", "range"}, func() []string {
			c := NewGetCmd()
			var got []string
			for _, f := range []string{"recursive", "concurrency", "part-size", "range"} {
				if c.Flags().Lookup(f) != nil {
					got = append(got, f)
				}
			}
			return got
		}},
		{"put", []string{"recursive", "concurrency", "part-size", "metadata"}, func() []string {
			c := NewPutCmd()
			var got []string
			for _, f := range []string{"recursive", "concurrency", "part-size", "metadata"} {
				if c.Flags().Lookup(f) != nil {
					got = append(got, f)
				}
			}
			return got
		}},
		{"mirror", []string{"remove", "overwrite", "dry-run", "concurrency", "part-size"}, func() []string {
			c := NewMirrorCmd()
			var got []string
			for _, f := range []string{"remove", "overwrite", "dry-run", "concurrency", "part-size"} {
				if c.Flags().Lookup(f) != nil {
					got = append(got, f)
				}
			}
			return got
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.check(); len(got) != len(tc.flags) {
				t.Fatalf("flags = %v, want %v", got, tc.flags)
			}
		})
	}
}

func TestExitCodeForError(t *testing.T) {
	if exitCodeForError(context.Canceled) != 5 {
		t.Fatal("cancel code")
	}
	if exitCodeForError(&s3api.ErrorResponse{StatusCode: 404}) != 3 {
		t.Fatal("not-found code")
	}
	if exitCodeForError(&s3api.ErrorResponse{StatusCode: 403}) != 4 {
		t.Fatal("access code")
	}
	if exitCodeForError(context.DeadlineExceeded) != 5 {
		t.Fatal("deadline code")
	}
}

func TestCommandContextsAndCancellation(t *testing.T) {
	ctx := newCmdContext(ParseS3PathAndLocalFile)
	if ctx.Global == nil || ctx.ArgParseMode != ParseS3PathAndLocalFile {
		t.Fatalf("context = %#v", ctx)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if !isCanceled(cancelled) {
		t.Fatal("cancelled context should be recognized")
	}
}
