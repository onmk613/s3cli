package s3api

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"io"
	"sort"
	"strings"
)

const (
	// emptySHA256Hex 是空请求体的 SHA256 十六进制值.
	emptySHA256Hex = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	// unsignedPayload 表示不对请求体做签名摘要 (流式上传等场景).
	unsignedPayload = "UNSIGNED-PAYLOAD"
)

// sortStrings 对字符串切片按字典序排序.
func sortStrings(s []string) {
	sort.Strings(s)
}

// hashSHA256Seeker 计算 ReadSeeker 内容的 SHA256, 并把位置回卷到起点.
func hashSHA256Seeker(r io.ReadSeeker) (string, error) {
	start, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	if _, err := r.Seek(start, io.SeekStart); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// sumHMACSHA256 计算 HMAC-SHA256.
func sumHMACSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

// sumSHA256Hex 计算数据的 SHA256 十六进制串.
func sumSHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func sumMD5Base64(data []byte) string {
	h := md5.New()
	h.Write(data)
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// percentEncode 按 AWS SigV4 / RFC 3986 规则对单个片段做百分号编码.
// 非保留字符: A-Z a-z 0-9 - _ . ~
func percentEncode(s string) string {
	var buf strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isUnreserved(c) {
			buf.WriteByte(c)
		} else {
			buf.WriteByte('%')
			buf.WriteString(strings.ToUpper(hex.EncodeToString([]byte{c})))
		}
	}
	return buf.String()
}

// encodePath 对对象 key 做路径编码: 逐段 percentEncode, 保留 '/' 分隔符.
func encodePath(p string) string {
	segments := strings.Split(p, "/")
	for i, seg := range segments {
		segments[i] = percentEncode(seg)
	}
	return strings.Join(segments, "/")
}

func isUnreserved(c byte) bool {
	return (c >= 'A' && c <= 'Z') ||
		(c >= 'a' && c <= 'z') ||
		(c >= '0' && c <= '9') ||
		c == '-' || c == '_' || c == '.' || c == '~'
}

// xmlDecoder provide decoded value in xml
func xmlDecoder(body io.Reader, v interface{}) error {
	d := xml.NewDecoder(body)
	return d.Decode(v)
}
