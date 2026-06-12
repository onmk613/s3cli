package utils

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ParseBytes 解析人类可读的字节大小，支持后缀 K/M/G/T/P（不区分大小写，
// 可带可不带 "B"，如 "4K"、"4KB"、"4096"、"1M"）。按 1024 进制。
// 纯数字（无后缀）按字节解释。返回的字节数为非负整数。
func ParseBytes(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty size")
	}

	upper := strings.ToUpper(s)
	upper = strings.TrimSuffix(upper, "B") // 允许 "KB"/"MB" 等写法

	mult := int64(1)
	if n := len(upper); n > 0 {
		switch upper[n-1] {
		case 'K':
			mult = 1 << 10
		case 'M':
			mult = 1 << 20
		case 'G':
			mult = 1 << 30
		case 'T':
			mult = 1 << 40
		case 'P':
			mult = 1 << 50
		}
		if mult != 1 {
			upper = upper[:n-1]
		}
	}

	upper = strings.TrimSpace(upper)
	val, err := strconv.ParseInt(upper, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q: %w", s, err)
	}
	if val < 0 {
		return 0, fmt.Errorf("invalid size %q: must be non-negative", s)
	}
	return val * mult, nil
}

// 格式化字节
func FormatBytes(bytes int64) string {
	if bytes <= 0 {
		return "0 B"
	}
	units := []string{"B", "KB", "MB", "GB", "TB", "PB", "EB"}
	base := 1024.0
	exp := int(math.Log(float64(bytes)) / math.Log(base))
	if exp >= len(units) {
		exp = len(units) - 1
	}
	value := float64(bytes) / math.Pow(base, float64(exp))
	return fmt.Sprintf("%.2f %s", value, units[exp])
}
