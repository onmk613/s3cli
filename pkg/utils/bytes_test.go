package utils

import (
	"fmt"
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
		{"-1", 0},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s", tt.input), func(t *testing.T) {
			result, _ := ParseBytes(tt.input)
			if result != tt.expected {
				t.Errorf("ParseBytes(%q) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{0, "0 B"},
		{1 << 0, "1.00 B"},
		{1 << 10, "1.00 KB"},
		{1 << 20, "1.00 MB"},
		{1 << 30, "1.00 GB"},
		{1 << 40, "1.00 TB"},
		{1 << 50, "1.00 PB"},
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
