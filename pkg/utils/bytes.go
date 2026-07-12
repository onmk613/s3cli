package utils

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ParseBytes 解析固定格式字符串为字节数
func ParseBytes(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty size")
	}

	upper := strings.ToUpper(s)
	upper = strings.TrimSuffix(upper, "B") // 允许 "KB"/"MB" 等写法

	m := int64(1)
	if n := len(upper); n > 0 {
		switch upper[n-1] {
		case 'K':
			m = 1 << 10
		case 'M':
			m = 1 << 20
		case 'G':
			m = 1 << 30
		case 'T':
			m = 1 << 40
		case 'P':
			m = 1 << 50
		}
		if m != 1 {
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
	return val * m, nil
}

// FormatBytes 格式化字节数
func FormatBytes(bytes int64) string {
	if bytes <= 0 {
		return "0 B"
	}
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	base := 1024.0
	exp := int(math.Log(float64(bytes)) / math.Log(base))
	if exp >= len(units) {
		exp = len(units) - 1
	}
	value := float64(bytes) / math.Pow(base, float64(exp))
	s := fmt.Sprintf("%.2f", value)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	return fmt.Sprintf("%s %s", s, units[exp])
}
