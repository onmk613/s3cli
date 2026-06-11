package utils

import (
	"fmt"
	"math"
)

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
