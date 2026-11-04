package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestConfirmBucketRemoval(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  bool
	}{
		{"exact", "prod-bucket\n", true},
		{"exact with spaces", "  prod-bucket  \n", true},
		{"wrong", "prod\n", false},
		{"empty", "\n", false},
		{"eof without newline", "prod-bucket", true},
		{"case sensitive", "PROD-BUCKET\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if got := confirmBucketRemoval("prod-bucket", strings.NewReader(tc.input), &out); got != tc.want {
				t.Fatalf("confirmBucketRemoval(%q) = %v, want %v", tc.input, got, tc.want)
			}
			if !strings.Contains(out.String(), "prod-bucket") {
				t.Fatalf("prompt should mention bucket name, got %q", out.String())
			}
		})
	}
}
