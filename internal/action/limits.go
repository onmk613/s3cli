// limits.go 集中定义并校验用户可调的数值型参数上界。
//
// 背景: --concurrency 与 --part-size 直接决定 channel 容量、goroutine 数量与
// 分片缓冲区大小。此前二者都没有上界校验:
//   - `get -r a:b/ out --concurrency 9223372036854775807` 在
//     make(chan StreamJob, cfg.Concurrency*2) 处直接 panic (makechan: size out
//     of range) 并把 goroutine 栈回溯打给用户;
//   - 稍小的值 (如 10^8) 不 panic 但会尝试申请同等规模的缓冲区, 直接 OOM;
//   - `--part-size` 只被钳到 S3 协议上限 5GiB, 配合每对象 4 路并发分片
//     (multipartConcurrency), 峰值内存可达 ~20GiB。
//
// 这些值全部来自用户输入 (flag / 别名配置 / 环境变量), 属于必须在边界处
// fail-fast 的范畴。上界取"远超任何合理用法、但不会打爆进程"的量级。

package action

import (
	"fmt"

	"s3cli/internal/i18n"
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
