package fmtutil

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ParseBytes 解析固定格式字符串为字节数。
// 支持小数（如 "1.5GB"），结果四舍五入取整；整数输入行为与之前一致。
// 数值 * 倍数超出 int64 范围时返回明确错误。
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
	// 用 ParseFloat 以支持小数; 整数输入（如 "1024"）解析结果不变
	val, err := strconv.ParseFloat(upper, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q: %w", s, err)
	}
	if val < 0 {
		return 0, fmt.Errorf("invalid size %q: must be non-negative", s)
	}

	// val*倍数 可能溢出 int64: 先用 float64 计算再检查范围。
	// 注意 float64(math.MaxInt64) 会进位到 2^63, 因此以 2^63 为界
	// （2^63 在 float64 中精确表示; 任何 >= 2^63 的结果都超出 int64）。
	result := val * float64(m)
	if math.IsNaN(result) || result >= float64(1<<63) || result < 0 {
		return 0, fmt.Errorf("invalid size %q: value out of range", s)
	}
	return int64(math.Round(result)), nil
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
