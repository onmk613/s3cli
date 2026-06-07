// Package util 提供 s3cmd 的通用工具函数。
package utils

import "strings"

// NormalizePrefix 统一处理 s3:// 路径中的 prefix 后缀斜杠逻辑：
//   - 如果 path 以 "/" 结尾且 key 非空，追加 "/"（列出目录内容）
//   - 兼容 ceph: "//" 规范化回 "/"
func NormalizePrefix(path, key string) string {
	prefix := key
	if strings.HasSuffix(path, "/") && key != "" {
		prefix = key + "/"
	}
	if prefix == "//" {
		prefix = "/"
	}
	return prefix
}

// ResolveDestKey 计算 cp/mv 操作的目标 key。
//
// destPath 以 "/" 结尾 -> 目标视为目录，将 srcBase 拼接到 destKey 下。
// destKey 为空 -> 直接用 srcBase。
func ResolveDestKey(destPath, destKey, srcBase string) string {
	if strings.HasSuffix(destPath, "/") {
		if destKey == "" {
			return srcBase
		}
		if !strings.HasSuffix(destKey, "/") {
			destKey += "/"
		}
		return destKey + srcBase
	}
	if destKey == "" {
		return srcBase
	}
	return destKey
}
