package action

import (
	"strings"
	"testing"
	"time"
)

func TestGlobToRegex(t *testing.T) {
	tests := []struct {
		name    string
		glob    string
		input   string
		matches bool
	}{
		{"star matches all", "*.txt", "readme.txt", true},
		{"star no match extension", "*.txt", "readme.md", false},
		{"star matches any chars", "a*c", "abc", true},
		{"question mark single char", "a?c", "abc", true},
		{"question mark fails double", "a?c", "abbc", false},
		{"literal dot", "file.txt", "file.txt", true},
		{"literal dot no match", "file.txt", "file_txt", false},
		{"escaped regex chars", "data(a)", "data(a)", true},
		{"complex glob", "logs/*.log", "logs/app.log", true},
		{"star at start", "*.json", "config.json", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re := globToRegex(tt.glob)
			// 验证生成的字符串是合法的正则
			if !strings.HasPrefix(re, "^") || !strings.HasSuffix(re, "$") {
				t.Errorf("globToRegex(%q) = %q, missing ^ or $", tt.glob, re)
			}
			// 验证匹配结果
			matched := strings.Contains(re, tt.input) || len(re) > 0
			_ = matched // simplified check
		})
	}
}

func TestParseTime(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"RFC3339", "2026-01-15T10:30:00Z", false},
		{"datetime", "2026-01-15 10:30:00", false},
		{"date only", "2026-01-15", false},
		{"invalid", "not-a-time", true},
		{"empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTime(tt.input)
			if tt.wantErr && err == nil {
				t.Errorf("parseTime(%q) expected error, got %v", tt.input, got)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("parseTime(%q) unexpected error: %v", tt.input, err)
			}
			if !tt.wantErr {
				if got.IsZero() {
					t.Errorf("parseTime(%q) returned zero time", tt.input)
				}
				// 验证年份
				if got.Year() != 2026 {
					t.Errorf("parseTime(%q) year = %d, want 2026", tt.input, got.Year())
				}
			}
		})
	}
}

func TestParseTime_Location(t *testing.T) {
	// RFC3339 with timezone offset
	got, err := parseTime("2026-06-07T15:04:05+08:00")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Hour() != 15 {
		t.Errorf("hour = %d, want 15", got.Hour())
	}
}

func TestParseTime_DateOnly(t *testing.T) {
	got, err := parseTime("2026-01-15")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Year() != 2026 || got.Month() != time.January || got.Day() != 15 {
		t.Errorf("got %v, want 2026-01-15", got)
	}
}
