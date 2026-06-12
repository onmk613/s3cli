package utils

import "testing"

func TestParseBytes(t *testing.T) {
	cases := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"4096", 4096, false},
		{"4K", 4096, false},
		{"4k", 4096, false},
		{"4KB", 4096, false},
		{"4kb", 4096, false},
		{"1M", 1 << 20, false},
		{"1G", 1 << 30, false},
		{"1T", 1 << 40, false},
		{"1P", 1 << 50, false},
		{"0", 0, false},
		{" 4K ", 4096, false},
		{"", 0, true},
		{"abc", 0, true},
		{"-1", 0, true},
		{"4X", 0, true},
	}
	for _, c := range cases {
		got, err := ParseBytes(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseBytes(%q): expected error, got %d", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseBytes(%q): unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseBytes(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}
