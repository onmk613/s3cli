package action

import (
	"fmt"

	"github.com/onmk613/s3cli/pkg/i18n"
)

const (
	// MaxConcurrency 是 --concurrency 的上界。1024 路并发文件传输已远超
	// 单机可用带宽与文件描述符的合理范围。
	MaxConcurrency = 1024

	// MaxPartSizeMB 是 --part-size / multipart_chunk_size_mb 的上界 (1GiB)。
	// S3 协议允许单片到 5GiB, 但本实现每个在途分片持有一份完整内存缓冲,
	// 且单对象内 4 路并发 —— 1GiB 已对应约 4GiB 峰值内存。
	MaxPartSizeMB = 1024
)

// validateConcurrency 校验并发数上界。<=0 表示"用默认值", 由调用方处理, 不算错误。
func validateConcurrency(n int) error {
	if n > MaxConcurrency {
		return fmt.Errorf(i18n.T(
			"invalid concurrency %d (allowed range: 1-%d)",
			"无效的并发数 %d（允许范围：1-%d）"), n, MaxConcurrency)
	}
	return nil
}

// validatePartSizeMB 校验分片大小上界。0 表示"用默认值", 合法。
func validatePartSizeMB(mb int) error {
	if mb > MaxPartSizeMB {
		return fmt.Errorf(i18n.T(
			"invalid part size %d MB (maximum %d MB; each in-flight part holds a full in-memory buffer)",
			"无效的分片大小 %d MB（最大 %d MB；每个在途分片都持有一份完整内存缓冲）"), mb, MaxPartSizeMB)
	}
	return nil
}
