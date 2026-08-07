// utils.go 提供包内复用的通用工具: SHA256/HMAC/MD5 哈希计算、RFC 3986 百分号编码、
// XML 编解码辅助、字符串处理 (trimQuotes / parseInt64 / indexOfByte),
// 以及 S3 桶名严格校验.

package api

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	// emptySHA256Hex 是空请求体的 SHA256 十六进制值.
	emptySHA256Hex = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	// unsignedPayload 表示不对请求体做签名摘要 (流式上传等场景).
	unsignedPayload = "UNSIGNED-PAYLOAD"
)

// defaultXMLNS 是 S3 配置接口的标准命名空间 (规范为 http://, 不是 https://,
// aws-sdk / minio-go 均如此; 写错会被严格校验的服务端拒为 MalformedXML)。
const defaultXMLNS = "http://s3.amazonaws.com/doc/2006-03-01/"

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
func xmlDecoder(body io.Reader, v any) error {
	d := xml.NewDecoder(body)
	return d.Decode(v)
}

// marshalXMLWithHeader 序列化为 XML 并加上 XML 声明头.
func marshalXMLWithHeader(v any) ([]byte, error) {
	body, err := xml.Marshal(v)
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), body...), nil
}

// trimQuotes 去掉字符串首尾的引号, 常用于清理 ETag 字段两端的引号.
func trimQuotes(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

// parseInt64 安全解析 int64; 解析失败时返回 0 而非 error.
func parseInt64(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

// indexOfByte 返回字节 b 在字符串 s 中首次出现的位置, 不存在返回 -1.
func indexOfByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// checkValidBucketNameStrict 检查 S3 桶名是否符合严格规则.
func checkValidBucketNameStrict(bucketName string) error {
	var (
		bucketNameRegex = regexp.MustCompile(`^[A-Za-z0-9][a-zA-Z0-9_\-.]{1,61}[A-Za-z0-9]$`)
		ipAddress       = regexp.MustCompile(`^(\d+\.){3}\d+$`)
	)

	if strings.TrimSpace(bucketName) == "" {
		return errors.New("bucket name cannot be empty")
	}
	if len(bucketName) < 3 {
		return errors.New("bucket name cannot be shorter than 3 characters")
	}
	if len(bucketName) > 63 {
		return errors.New("bucket name cannot be longer than 63 characters")
	}
	if ipAddress.MatchString(bucketName) {
		return errors.New("bucket name cannot be an ip address")
	}
	if strings.Contains(bucketName, "..") || strings.Contains(bucketName, ".-") || strings.Contains(bucketName, "-.") {
		return errors.New("bucket name contains invalid characters")
	}
	if !bucketNameRegex.MatchString(bucketName) {
		return errors.New("bucket name contains invalid characters")
	}
	return nil
}
