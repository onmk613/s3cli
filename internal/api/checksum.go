// checksum.go 实现 S3 的附加校验和 (additional checksums):
// x-amz-checksum-<alg> / x-amz-sdk-checksum-algorithm / x-amz-checksum-mode.
//
// 背景: 项目此前只发 Content-MD5, 没有任何附加校验和 —— 下载端更是完全不校验
// 完整性 (io.Copy 完直接 rename), 上传端在分片路径上也无从回传分片校验和。
// 新版 AWS SDK 默认带 CRC64NVME/CRC32, 部分兼容实现会强校验, 缺失时会直接拒绝
// 或让这类客户端无法互操作。
//
// 支持算法: CRC32 / CRC32C / SHA1 / SHA256 (与 AWS 的
// x-amz-sdk-checksum-algorithm 取值一致)。

package api

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"strings"
)

// ChecksumAlgorithm 是附加校验和算法名 (S3 协议取值)。
type ChecksumAlgorithm string

const (
	ChecksumCRC32  ChecksumAlgorithm = "CRC32"
	ChecksumCRC32C ChecksumAlgorithm = "CRC32C"
	ChecksumSHA1   ChecksumAlgorithm = "SHA1"
	ChecksumSHA256 ChecksumAlgorithm = "SHA256"
)

// ParseChecksumAlgorithm 归一化用户输入的算法名, 空串表示"不启用"。
// 无法识别时返回 ok=false, 由调用方决定报错还是忽略。
func ParseChecksumAlgorithm(s string) (ChecksumAlgorithm, bool) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "":
		return "", true
	case "CRC32":
		return ChecksumCRC32, true
	case "CRC32C":
		return ChecksumCRC32C, true
	case "SHA1":
		return ChecksumSHA1, true
	case "SHA256":
		return ChecksumSHA256, true
	default:
		return "", false
	}
}

// headerName 返回该算法在请求/响应里使用的头名后缀, 如 "crc32"。
func (a ChecksumAlgorithm) headerName() string {
	return strings.ToLower(string(a))
}

// newHash 返回该算法对应的 hash.Hash; 空算法或未知算法返回 nil。
func (a ChecksumAlgorithm) newHash() hash.Hash {
	switch a {
	case ChecksumCRC32:
		return crc32.NewIEEE()
	case ChecksumCRC32C:
		return crc32.New(crc32.MakeTable(crc32.Castagnoli))
	case ChecksumSHA1:
		return sha1.New()
	case ChecksumSHA256:
		return sha256.New()
	default:
		return nil
	}
}

// computeChecksum 计算 data 的附加校验和, 返回 base64 编码值。
// 空算法返回 ""。
func (a ChecksumAlgorithm) computeChecksum(data []byte) string {
	h := a.newHash()
	if h == nil {
		return ""
	}
	h.Write(data)
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// ComputeChecksumReader 流式计算 reader 内容的附加校验和 (base64 编码)。
// 供下载后校验使用, 不需要把内容整体读入内存。
func ComputeChecksumReader(a ChecksumAlgorithm, r io.Reader) (string, error) {
	h := a.newHash()
	if h == nil {
		return "", fmt.Errorf("unsupported checksum algorithm %q", a)
	}
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(h.Sum(nil)), nil
}

// ChecksumValues 汇总一次响应里出现的全部附加校验和 (未出现的为空串)。
type ChecksumValues struct {
	CRC32  string
	CRC32C string
	SHA1   string
	SHA256 string
}

// Any 报告是否至少有一个校验和存在。
func (c ChecksumValues) Any() bool {
	return c.CRC32 != "" || c.CRC32C != "" || c.SHA1 != "" || c.SHA256 != ""
}

// Get 按算法取值。
func (c ChecksumValues) Get(a ChecksumAlgorithm) string {
	switch a {
	case ChecksumCRC32:
		return c.CRC32
	case ChecksumCRC32C:
		return c.CRC32C
	case ChecksumSHA1:
		return c.SHA1
	case ChecksumSHA256:
		return c.SHA256
	default:
		return ""
	}
}

// checksumValuesFromHeader 从响应头解析附加校验和。
func checksumValuesFromHeader(h interface{ Get(string) string }) ChecksumValues {
	return ChecksumValues{
		CRC32:  h.Get("x-amz-checksum-crc32"),
		CRC32C: h.Get("x-amz-checksum-crc32c"),
		SHA1:   h.Get("x-amz-checksum-sha1"),
		SHA256: h.Get("x-amz-checksum-sha256"),
	}
}
