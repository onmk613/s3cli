package fmtutil

import (
	"fmt"
	"strings"
	"testing"
)

func TestParseBytes(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
	}{
		{"1024", 1 << 10},
		{"1K", 1 << 10},
		{"1M", 1 << 20},
		{"1G", 1 << 30},
		{"1T", 1 << 40},
		{"1P", 1 << 50},
		{"1024B", 1 << 10},
		{"1KB", 1 << 10},
		{"1MB", 1 << 20},
		{"1GB", 1 << 30},
		{"1TB", 1 << 40},
		{"1PB", 1 << 50},
		{"1.5GB", 1610612736},          // 1.5 * 2^30
		{"0.5K", 512},                  // 小数四舍五入
		{"8191P", 9222246136947933184}, // 恰好落在 int64 范围内的最大 PB 数
		{"-1", 0},
		{"-1.5GB", 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, _ := ParseBytes(tt.input)
			if result != tt.expected {
				t.Errorf("ParseBytes(%q) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseBytesOverflow(t *testing.T) {
	// val*倍数 超出 int64 范围时必须返回明确错误
	cases := []string{
		"999999999999PB",       // ~1.1e27 远超 int64 上限
		"8192P",                // 2^63, 刚好越界
		"1e18G",                // 指数写法
		"9223372036854775807B", // ParseFloat 无法精确表示 MaxInt64, 视为越界
		"1e308",
	}
	for _, input := range cases {
		_, err := ParseBytes(input)
		if err == nil {
			t.Errorf("ParseBytes(%q) should error (out of range)", input)
			continue
		}
		if !strings.Contains(err.Error(), "out of range") {
			t.Errorf("ParseBytes(%q) error = %v, want mention of out of range", input, err)
		}
	}
}

func TestParseBytesInvalid(t *testing.T) {
	for _, input := range []string{"abc", "1.2.3", ""} {
		if _, err := ParseBytes(input); err == nil {
			t.Errorf("ParseBytes(%q) should error", input)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{0, "0 B"},
		{1 << 0, "1 B"},
		{1 << 10, "1 KB"},
		{1 << 20, "1 MB"},
		{1 << 30, "1 GB"},
		{1 << 40, "1 TB"},
		{1 << 50, "1 PB"},
		{-1, "0 B"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d", tt.bytes), func(t *testing.T) {
			result := FormatBytes(tt.bytes)
			if result != tt.expected {
				t.Errorf("FormatBytes(%d) = %s, want %s", tt.bytes, result, tt.expected)
			}
		})
	}
}
