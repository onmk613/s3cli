// Package action 提供 S3 操作的默认值，集中管理避免硬编码散落。
package action

import "s3cli/pkg/config"

// 所有命令共享的默认参数，引用自 config 包。
// 调用方可通过命令行 flag 覆盖。
const (
	DefaultConcurrency       = config.DefaultConcurrency
	DefaultPartSizeMB        = config.DefaultPartSizeMB
	DefaultMimeType          = config.DefaultMimeType
	DefaultMirrorConcurrency = 8
	DefaultDiffConcurrency   = 16
)
