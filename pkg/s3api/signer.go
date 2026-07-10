package s3api

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// AWS Signature Version 4 实现 (纯标准库, 不依赖 SDK).

const (
	signV4Algorithm = "AWS4-HMAC-SHA256"
	serviceS3       = "s3"
	iso8601Format   = "20060102T150405Z"
	yyyymmddFormat  = "20060102"
)

// 这些头不参与签名 (随代理/网络层变化).
var ignoredSigningHeaders = map[string]struct{}{
	"Authorization":   {},
	"User-Agent":      {},
	"Accept-Encoding": {},
}

// signV4 对请求做 SigV4 header 签名, 直接修改 req 的 Header.
// https://docs.aws.amazon.com/AmazonS3/latest/developerguide/sigv4-auth-using-authorization-header.html
func signV4(req *http.Request, accessKey, secretKey, region, payloadSHA256Hex string, t time.Time) {
	amzDate := t.Format(iso8601Format)
	scopeDate := t.Format(yyyymmddFormat)

	req.Header.Set("X-Amz-Date", amzDate)
	if req.Header.Get("Host") == "" {
		req.Host = req.URL.Host
	}

	canonicalRequest, signedHeaders := buildCanonicalRequest(req, payloadSHA256Hex)

	scope := strings.Join([]string{scopeDate, region, serviceS3, "aws4_request"}, "/")
	stringToSign := strings.Join([]string{
		signV4Algorithm,
		amzDate,
		scope,
		sumSHA256Hex([]byte(canonicalRequest)),
	}, "\n")

	signingKey := deriveSigningKey(secretKey, scopeDate, region)
	signature := hexHMAC(signingKey, stringToSign)

	authorization := signV4Algorithm +
		" Credential=" + accessKey + "/" + scope +
		", SignedHeaders=" + signedHeaders +
		", Signature=" + signature

	req.Header.Set("Authorization", authorization)
}

// buildCanonicalRequest 构建规范请求串, 返回 (canonicalRequest, signedHeaders).
func buildCanonicalRequest(req *http.Request, payloadSHA256Hex string) (string, string) {
	// 1. 规范 URI
	canonicalURI := req.URL.EscapedPath()
	if canonicalURI == "" {
		canonicalURI = "/"
	}

	// 2. 规范查询串: 键值均编码, 按键排序
	canonicalQuery := canonicalQueryString(req.URL.Query())

	// 3. 规范头: 小写键, 值去首尾空白, 按键排序
	headerKeys := make([]string, 0, len(req.Header)+1)
	headerMap := make(map[string]string, len(req.Header)+1)

	headerKeys = append(headerKeys, "host")
	host := req.Host
	if host == "" {
		host = req.URL.Host
	}
	headerMap["host"] = host

	// Go 将 Content-Length 存于 req.ContentLength 而非 Header, 需单独纳入签名
	if req.ContentLength > 0 {
		headerKeys = append(headerKeys, "content-length")
		headerMap["content-length"] = strconv.FormatInt(req.ContentLength, 10)
	}

	for k, vv := range req.Header {
		if _, skip := ignoredSigningHeaders[http.CanonicalHeaderKey(k)]; skip {
			continue
		}
		lk := strings.ToLower(k)
		vals := make([]string, len(vv))
		for i, v := range vv {
			vals[i] = strings.TrimSpace(v)
		}
		headerMap[lk] = strings.Join(vals, ",")
		headerKeys = append(headerKeys, lk)
	}
	sortStrings(headerKeys)

	var canonicalHeaders strings.Builder
	for _, k := range headerKeys {
		canonicalHeaders.WriteString(k)
		canonicalHeaders.WriteByte(':')
		canonicalHeaders.WriteString(headerMap[k])
		canonicalHeaders.WriteByte('\n')
	}
	signedHeaders := strings.Join(headerKeys, ";")

	if payloadSHA256Hex == "" {
		payloadSHA256Hex = unsignedPayload
	}

	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI,
		canonicalQuery,
		canonicalHeaders.String(),
		signedHeaders,
		payloadSHA256Hex,
	}, "\n")

	return canonicalRequest, signedHeaders
}

// canonicalQueryString 生成 SigV4 规范查询串.
func canonicalQueryString(v url.Values) string {
	if len(v) == 0 {
		return ""
	}
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sortStrings(keys)

	var buf strings.Builder
	for _, k := range keys {
		vals := append([]string(nil), v[k]...)
		sortStrings(vals)
		for _, val := range vals {
			if buf.Len() > 0 {
				buf.WriteByte('&')
			}
			buf.WriteString(percentEncode(k))
			buf.WriteByte('=')
			buf.WriteString(percentEncode(val))
		}
	}
	return buf.String()
}

// deriveSigningKey 派生 SigV4 签名密钥.
func deriveSigningKey(secretKey, scopeDate, region string) []byte {
	dateKey := sumHMACSHA256([]byte("AWS4"+secretKey), []byte(scopeDate))
	regionKey := sumHMACSHA256(dateKey, []byte(region))
	serviceKey := sumHMACSHA256(regionKey, []byte(serviceS3))
	return sumHMACSHA256(serviceKey, []byte("aws4_request"))
}

func hexHMAC(key []byte, data string) string {
	return sumSHA256HexOfHMAC(key, data)
}

func sumSHA256HexOfHMAC(key []byte, data string) string {
	mac := sumHMACSHA256(key, []byte(data))
	const hextable = "0123456789abcdef"
	out := make([]byte, len(mac)*2)
	for i, b := range mac {
		out[i*2] = hextable[b>>4]
		out[i*2+1] = hextable[b&0x0f]
	}
	return string(out)
}
