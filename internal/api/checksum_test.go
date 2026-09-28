// checksum_test.go 覆盖附加校验和的计算路径。
//
// 此前 checksum.go 的 headerName / newHash / computeChecksum /
// ComputeChecksumReader / Any / Get 以及 CompletedPart.SetChecksum 全部为 0% 覆盖
// —— 而它们是 `put --checksum` 与 `get --checksum` 的数据完整性核心。
// 已有测试只覆盖算法名解析 (ParseChecksumAlgorithm), 恰好在"算得对不对"这一半是空的。
//
// 期望值取自公开的标准校验向量 (输入固定为 "123456789"):
//
//	CRC32  (IEEE)       = 0xCBF43926
//	CRC32C (Castagnoli) = 0xE3069283
//
// SHA1 / SHA256 为 FIPS 180 标准向量。base64 值由 Python 独立算出。

package api

import (
	"bytes"
	"encoding/base64"
	"errors"
	"hash/crc32"
	"io"
	"net/http"
	"strings"
	"testing"
)

// checksumVectorInput 是标准校验向量使用的输入。
const checksumVectorInput = "123456789"

var checksumVectors = map[ChecksumAlgorithm]string{
	ChecksumCRC32:  "y/Q5Jg==",
	ChecksumCRC32C: "4waSgw==",
	ChecksumSHA1:   "98O8HYCOBHMq32eZZczDTKeuNEE=",
	ChecksumSHA256: "FeKw08M4keuw8e9gnsQZQgwg4yDOlMZfvIwzEkSOsiU=",
}

func TestComputeChecksumMatchesStandardVectors(t *testing.T) {
	for alg, want := range checksumVectors {
		t.Run(string(alg), func(t *testing.T) {
			if got := alg.computeChecksum([]byte(checksumVectorInput)); got != want {
				t.Fatalf("%s checksum = %q, want %q", alg, got, want)
			}
		})
	}
}

func TestComputeChecksumEmptyAlgorithm(t *testing.T) {
	var empty ChecksumAlgorithm
	if got := empty.computeChecksum([]byte("data")); got != "" {
		t.Fatalf("empty algorithm = %q, want empty string", got)
	}
	if h := empty.newHash(); h != nil {
		t.Fatal("empty algorithm must have no hash")
	}
	if got := ChecksumAlgorithm("MD5").computeChecksum([]byte("data")); got != "" {
		t.Fatalf("unsupported algorithm = %q, want empty string", got)
	}
}

// TestComputeChecksumReaderMatchesInMemory 流式与一次性计算必须一致 ——
// 下载校验走流式路径 (避免整对象入内存), 上传走一次性路径, 二者口径必须相同。
func TestComputeChecksumReaderMatchesInMemory(t *testing.T) {
	for alg, want := range checksumVectors {
		t.Run(string(alg), func(t *testing.T) {
			got, err := ComputeChecksumReader(alg, strings.NewReader(checksumVectorInput))
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("streaming %s = %q, want %q", alg, got, want)
			}
			if inMem := alg.computeChecksum([]byte(checksumVectorInput)); inMem != got {
				t.Fatalf("streaming %q != in-memory %q", got, inMem)
			}
		})
	}
}

// chunkedReader 每次只交付少量字节, 用于确认实现是流式消费而非整体缓冲。
type chunkedReader struct {
	data  []byte
	chunk int
	pos   int
}

func (r *chunkedReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := r.chunk
	if n > len(p) {
		n = len(p)
	}
	if r.pos+n > len(r.data) {
		n = len(r.data) - r.pos
	}
	copy(p, r.data[r.pos:r.pos+n])
	r.pos += n
	return n, nil
}

func TestComputeChecksumReaderIsStreaming(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), 1<<20)
	got, err := ComputeChecksumReader(ChecksumSHA256, &chunkedReader{data: payload, chunk: 7})
	if err != nil {
		t.Fatal(err)
	}
	if want := ChecksumSHA256.computeChecksum(payload); got != want {
		t.Fatalf("chunked read = %q, want %q", got, want)
	}
	if _, err := ComputeChecksumReader(ChecksumSHA256, bytes.NewReader(nil)); err != nil {
		t.Fatalf("empty input must be handled: %v", err)
	}
}

// readErrorReader 在读取中途失败, 用于确认错误被上抛, 而不是被吞掉后返回一个
// "看起来合法"的校验和 —— 那会让下载校验静默失效。
type readErrorReader struct{ n int }

func (r *readErrorReader) Read(p []byte) (int, error) {
	if r.n == 0 {
		r.n++
		copy(p, "partial")
		return len("partial"), nil
	}
	return 0, errors.New("simulated read failure")
}

func TestComputeChecksumReaderPropagatesReadError(t *testing.T) {
	if _, err := ComputeChecksumReader(ChecksumSHA256, &readErrorReader{}); err == nil {
		t.Fatal("read error must be propagated, not silently turned into a checksum")
	}
}

func TestComputeChecksumReaderUnsupportedAlgorithm(t *testing.T) {
	if _, err := ComputeChecksumReader("MD5", strings.NewReader("x")); err == nil {
		t.Fatal("unsupported algorithm must return an error")
	}
	var empty ChecksumAlgorithm
	if _, err := ComputeChecksumReader(empty, strings.NewReader("x")); err == nil {
		t.Fatal("empty algorithm must return an error")
	}
}

func TestChecksumAlgorithmHeaderName(t *testing.T) {
	for alg, want := range map[ChecksumAlgorithm]string{
		ChecksumCRC32:  "crc32",
		ChecksumCRC32C: "crc32c",
		ChecksumSHA1:   "sha1",
		ChecksumSHA256: "sha256",
	} {
		if got := alg.headerName(); got != want {
			t.Errorf("headerName(%s) = %q, want %q", alg, got, want)
		}
	}
}

// TestNewHashAlgorithmsDistinct CRC32 与 CRC32C 用的是不同的多项式表,
// 混淆会让服务端校验必然失败 (且只在对端强校验时才暴露)。
func TestNewHashAlgorithmsDistinct(t *testing.T) {
	ieee := ChecksumCRC32.computeChecksum([]byte(checksumVectorInput))
	castagnoli := ChecksumCRC32C.computeChecksum([]byte(checksumVectorInput))
	if ieee == castagnoli {
		t.Fatal("CRC32 and CRC32C must use different polynomials")
	}
	// 与标准库 IEEE 实现对照, 确认不是"两个都算错但恰好不同"。
	var be [4]byte
	v := crc32.ChecksumIEEE([]byte(checksumVectorInput))
	be[0], be[1], be[2], be[3] = byte(v>>24), byte(v>>16), byte(v>>8), byte(v)
	if want := base64.StdEncoding.EncodeToString(be[:]); ieee != want {
		t.Fatalf("CRC32 = %q, want IEEE %q", ieee, want)
	}
	if ChecksumCRC32.newHash().Size() != 4 || ChecksumCRC32C.newHash().Size() != 4 {
		t.Fatal("CRC32 variants must be 4 bytes")
	}
	if ChecksumSHA1.newHash().Size() != 20 || ChecksumSHA256.newHash().Size() != 32 {
		t.Fatal("SHA1/SHA256 digest sizes must be 20/32")
	}
}

// TestChecksumValuesAnyAndGet 校验分值槽互不串味 ——
// 串味会让 `get --checksum` 用另一种算法的值去比对, 静默失去校验意义。
func TestChecksumValuesAnyAndGet(t *testing.T) {
	var none ChecksumValues
	if none.Any() {
		t.Fatal("zero value must report Any() == false")
	}

	cases := []struct {
		alg ChecksumAlgorithm
		set func(*ChecksumValues, string)
	}{
		{ChecksumCRC32, func(v *ChecksumValues, s string) { v.CRC32 = s }},
		{ChecksumCRC32C, func(v *ChecksumValues, s string) { v.CRC32C = s }},
		{ChecksumSHA1, func(v *ChecksumValues, s string) { v.SHA1 = s }},
		{ChecksumSHA256, func(v *ChecksumValues, s string) { v.SHA256 = s }},
	}
	for _, tc := range cases {
		var v ChecksumValues
		tc.set(&v, "value-"+string(tc.alg))
		if !v.Any() {
			t.Errorf("Any() = false after setting %s", tc.alg)
		}
		if got := v.Get(tc.alg); got != "value-"+string(tc.alg) {
			t.Errorf("Get(%s) = %q", tc.alg, got)
		}
		for _, other := range cases {
			if other.alg == tc.alg {
				continue
			}
			if got := v.Get(other.alg); got != "" {
				t.Errorf("Get(%s) = %q after only setting %s; slots must be independent", other.alg, got, tc.alg)
			}
		}
	}
	if got := none.Get("MD5"); got != "" {
		t.Errorf("Get(unknown) = %q, want empty", got)
	}
}

// TestCompletedPartSetChecksum 覆盖分片上报校验和: 必须只填对应算法的槽,
// 并清空其余槽 (上一片残留的值会污染本次 CompleteMultipartUpload)。
func TestCompletedPartSetChecksum(t *testing.T) {
	dirty := func() *CompletedPart {
		return &CompletedPart{
			PartNumber: 1, ETag: `"e"`,
			ChecksumCRC32: "stale", ChecksumCRC32C: "stale",
			ChecksumSHA1: "stale", ChecksumSHA256: "stale",
		}
	}
	values := ChecksumValues{CRC32: "c32", CRC32C: "c32c", SHA1: "s1", SHA256: "s256"}

	expect := []struct {
		alg  ChecksumAlgorithm
		want string
	}{
		{ChecksumCRC32, values.CRC32},
		{ChecksumCRC32C, values.CRC32C},
		{ChecksumSHA1, values.SHA1},
		{ChecksumSHA256, values.SHA256},
	}
	for _, tc := range expect {
		t.Run(string(tc.alg), func(t *testing.T) {
			p := dirty()
			p.SetChecksum(tc.alg, values)
			got := map[ChecksumAlgorithm]string{
				ChecksumCRC32:  p.ChecksumCRC32,
				ChecksumCRC32C: p.ChecksumCRC32C,
				ChecksumSHA1:   p.ChecksumSHA1,
				ChecksumSHA256: p.ChecksumSHA256,
			}
			if got[tc.alg] != tc.want {
				t.Errorf("%s slot = %q, want %q", tc.alg, got[tc.alg], tc.want)
			}
			for alg, v := range got {
				if alg == tc.alg {
					continue
				}
				if v != "" {
					t.Errorf("%s slot = %q, want empty (must be cleared)", alg, v)
				}
			}
		})
	}

	// 未知算法: 全部清空, 不留下上一片的值。
	p := dirty()
	p.SetChecksum("MD5", values)
	if p.ChecksumCRC32 != "" || p.ChecksumCRC32C != "" || p.ChecksumSHA1 != "" || p.ChecksumSHA256 != "" {
		t.Fatalf("unknown algorithm must clear all slots, got %+v", p)
	}
}

// TestChecksumValuesFromHeader 覆盖响应头解析 (--checksum 下载校验的入口)。
func TestChecksumValuesFromHeader(t *testing.T) {
	h := http.Header{}
	h.Set("x-amz-checksum-crc32", "AAAAAA==")
	h.Set("x-amz-checksum-sha256", "BBBB")
	v := checksumValuesFromHeader(h)
	if v.CRC32 != "AAAAAA==" || v.SHA256 != "BBBB" {
		t.Fatalf("parsed = %+v", v)
	}
	if v.CRC32C != "" || v.SHA1 != "" {
		t.Fatalf("absent headers must be empty: %+v", v)
	}
	if !v.Any() {
		t.Fatal("Any() should be true")
	}
	if got := checksumValuesFromHeader(http.Header{}); got.Any() {
		t.Fatalf("empty headers must yield no checksums: %+v", got)
	}
}

// TestParseChecksumAlgorithmNormalization 覆盖大小写/空白归一化与拒绝路径。
func TestParseChecksumAlgorithmNormalization(t *testing.T) {
	for _, in := range []string{"", "  ", "crc32", "CRC32", " crc32c ", "Sha1", "sha256"} {
		if _, ok := ParseChecksumAlgorithm(in); !ok {
			t.Errorf("ParseChecksumAlgorithm(%q) should be accepted", in)
		}
	}
	for _, in := range []string{"md5", "crc64nvme", "sha512", "crc32x"} {
		if _, ok := ParseChecksumAlgorithm(in); ok {
			t.Errorf("ParseChecksumAlgorithm(%q) should be rejected", in)
		}
	}
}
