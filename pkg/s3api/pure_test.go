package s3api

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestPercentEncode(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"abc-_.~123", "abc-_.~123"}, // 非保留字符原样
		{"a b", "a%20b"},             // 空格 -> %20
		{"+", "%2B"},
		{"/", "%2F"},
		{"中文", "%E4%B8%AD%E6%96%87"},
	}
	for _, tc := range cases {
		if got := percentEncode(tc.in); got != tc.want {
			t.Errorf("percentEncode(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestEncodePath(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"a/b", "a/b"},       // 斜杠保留
		{"a b/c", "a%20b/c"}, // 每段单独编码
		{"a/../b", "a/../b"}, // 分段编码, "." 是非保留字符
	}
	for _, tc := range cases {
		if got := encodePath(tc.in); got != tc.want {
			t.Errorf("encodePath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestIsUnreserved(t *testing.T) {
	good := []byte{'A', 'Z', 'a', 'z', '0', '9', '-', '_', '.', '~'}
	for _, c := range good {
		if !isUnreserved(c) {
			t.Errorf("expected %q unreserved", c)
		}
	}
	bad := []byte{' ', '/', '+', '@', 0x80, 0xff}
	for _, c := range bad {
		if isUnreserved(c) {
			t.Errorf("expected %q reserved", c)
		}
	}
}

func TestSumSHA256Hex(t *testing.T) {
	if got := sumSHA256Hex(nil); got != emptySHA256Hex {
		t.Errorf("empty sha256 = %q, want %q", got, emptySHA256Hex)
	}
	// 与 stdlib 对照
	sum := sha256.Sum256([]byte("hello"))
	want := hex.EncodeToString(sum[:])
	if got := sumSHA256Hex([]byte("hello")); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestSumMD5Base64(t *testing.T) {
	got := sumMD5Base64([]byte("hello"))
	if got != "XUFAKrxLKna5cZ2REBfFkg==" {
		t.Errorf("unexpected md5 base64: %q", got)
	}
}

func TestSumHMACSHA256(t *testing.T) {
	h := hmac.New(sha256.New, []byte("key"))
	h.Write([]byte("data"))
	want := h.Sum(nil)
	if got := sumHMACSHA256([]byte("key"), []byte("data")); !bytes.Equal(got, want) {
		t.Errorf("hmac mismatch")
	}
}

func TestHashSHA256Seeker(t *testing.T) {
	r := bytes.NewReader([]byte("some payload"))
	got, err := hashSHA256Seeker(r)
	if err != nil {
		t.Fatal(err)
	}
	// 位置应回卷到起点
	if pos, _ := r.Seek(0, io.SeekCurrent); pos != 0 {
		t.Errorf("reader not rewound, pos=%d", pos)
	}
	want := sumSHA256Hex([]byte("some payload"))
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
	// 空 reader
	got2, _ := hashSHA256Seeker(bytes.NewReader(nil))
	if got2 != emptySHA256Hex {
		t.Errorf("empty hash = %q", got2)
	}
}

func TestSortStrings(t *testing.T) {
	s := []string{"c", "a", "b"}
	sortStrings(s)
	want := []string{"a", "b", "c"}
	for i := range s {
		if s[i] != want[i] {
			t.Errorf("not sorted: %v", s)
			break
		}
	}
}

func TestXMLDecoder(t *testing.T) {
	var v struct {
		Name string `xml:"Name"`
	}
	if err := xmlDecoder(strings.NewReader("<X><Name>foo</Name></X>"), &v); err != nil {
		t.Fatal(err)
	}
	if v.Name != "foo" {
		t.Errorf("got %q", v.Name)
	}
	// 空内容 -> io.EOF 包装
	if err := xmlDecoder(strings.NewReader(""), &v); err == nil {
		t.Error("expected error for empty body")
	}
}

func TestCheckValidBucketNameStrict(t *testing.T) {
	good := []string{"abc", "my-bucket", "my.bucket", "bucket123", "My_Bucket", "a" + strings.Repeat("b", 60) + "c"}
	bad := []string{
		"", " ", "ab", // 太短
		strings.Repeat("a", 64),                  // 太长
		"1.2.3.4",                                // IP
		"my..bucket", "my.-bucket", "my-.bucket", // 非法组合
	}
	for _, b := range good {
		if err := checkValidBucketNameStrict(b); err != nil {
			t.Errorf("expected %q valid, got %v", b, err)
		}
	}
	for _, b := range bad {
		if err := checkValidBucketNameStrict(b); err == nil {
			t.Errorf("expected %q invalid", b)
		}
	}
}

func TestErrorResponseError(t *testing.T) {
	e := &ErrorResponse{Code: "NoSuchKey", Message: "missing", StatusCode: 404}
	s := e.Error()
	if !strings.Contains(s, "NoSuchKey") || !strings.Contains(s, "missing") {
		t.Errorf("unexpected error string: %s", s)
	}
}

func newErrResp(status int, body string, bucket, object string) *http.Response {
	r := &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	if bucket != "" {
		r.Header.Set("X-Amz-Request-Id", "req-1")
		r.Header.Set("X-Amz-Id-2", "host-2")
	}
	return r
}

func TestParseErrorResponse(t *testing.T) {
	t.Run("xml body", func(t *testing.T) {
		body := `<Error><Code>NoSuchKey</Code><Message>gone</Message></Error>`
		e := parseErrorResponse(newErrResp(404, body, "bk", "obj"), "bk", "obj")
		if e.Code != "NoSuchKey" || e.Message != "gone" {
			t.Errorf("got %+v", e)
		}
		if e.RequestID != "req-1" || e.HostID != "host-2" {
			t.Errorf("headers not backfilled: %+v", e)
		}
	})
	t.Run("404 object fallback", func(t *testing.T) {
		e := parseErrorResponse(newErrResp(404, "", "", "obj"), "", "obj")
		if e.Code != "NoSuchKey" {
			t.Errorf("got %q", e.Code)
		}
	})
	t.Run("404 bucket fallback", func(t *testing.T) {
		e := parseErrorResponse(newErrResp(404, "", "bk", ""), "bk", "")
		if e.Code != "NoSuchBucket" {
			t.Errorf("got %q", e.Code)
		}
	})
	t.Run("403 fallback", func(t *testing.T) {
		e := parseErrorResponse(newErrResp(403, "", "", ""), "", "")
		if e.Code != "AccessDenied" {
			t.Errorf("got %q", e.Code)
		}
	})
	t.Run("409 fallback", func(t *testing.T) {
		e := parseErrorResponse(newErrResp(409, "", "", ""), "", "")
		if e.Code != "Conflict" {
			t.Errorf("got %q", e.Code)
		}
	})
	t.Run("412 fallback", func(t *testing.T) {
		e := parseErrorResponse(newErrResp(412, "", "", ""), "", "")
		if e.Code != "PreconditionFailed" {
			t.Errorf("got %q", e.Code)
		}
	})
	t.Run("500 fallback", func(t *testing.T) {
		e := parseErrorResponse(newErrResp(500, "", "", ""), "", "")
		if e.Code == "" {
			t.Error("expected non-empty code")
		}
	})
	t.Run("backfill bucket/key", func(t *testing.T) {
		body := `<Error><Code>AccessDenied</Code></Error>`
		e := parseErrorResponse(newErrResp(403, body, "bk", "obj"), "bk", "obj")
		if e.BucketName != "bk" || e.Key != "obj" {
			t.Errorf("not backfilled: %+v", e)
		}
	})
}

func TestTrimQuotes(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""}, {"\"", "\""}, {"\"x\"", "x"}, {"\"x", "\"x"}, {"x\"", "x\""}, {"abc", "abc"},
	}
	for _, tc := range cases {
		if got := trimQuotes(tc.in); got != tc.want {
			t.Errorf("trimQuotes(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseInt64(t *testing.T) {
	if parseInt64("123") != 123 {
		t.Error("123")
	}
	if parseInt64("") != 0 {
		t.Error("empty")
	}
	if parseInt64("abc") != 0 {
		t.Error("abc")
	}
	if parseInt64("-5") != -5 {
		t.Error("negative")
	}
}

func TestParseHeadObjectHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("Content-Type", "image/png")
	h.Set("ETag", "\"abc123\"")
	h.Set("Content-Length", "1024")
	h.Set("Last-Modified", time.RFC1123)
	h.Set("x-amz-delete-marker", "true")
	h.Set("x-amz-meta-foo", "bar")
	h.Set("x-amz-version-id", "v1")

	lm, _ := time.Parse(time.RFC1123, time.RFC1123)
	out := parseHeadObjectHeaders(h)
	if out.ContentType != "image/png" {
		t.Errorf("ContentType=%q", out.ContentType)
	}
	if out.ETag != "abc123" {
		t.Errorf("ETag=%q want abc123 (quotes stripped)", out.ETag)
	}
	if out.ContentLength != 1024 {
		t.Errorf("ContentLength=%d", out.ContentLength)
	}
	if !out.LastModified.Equal(lm) {
		t.Errorf("LastModified mismatch")
	}
	if !out.DeleteMarker {
		t.Error("expected DeleteMarker true")
	}
	if out.VersionID != "v1" {
		t.Errorf("VersionID=%q", out.VersionID)
	}
	if out.Metadata["Foo"] != "bar" {
		t.Errorf("metadata=%+v", out.Metadata)
	}
}

func TestParseContentRangeLength(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"bytes 0-1023/2048", 1024},
		{"bytes0-1023/2048", 1024}, // 无空格
		{"bytes 5-5/10", 1},
		{"bytes 1023-0/2048", 0}, // 反转
		{"", 0},
	}
	for _, tc := range cases {
		if got := parseContentRangeLength(tc.in); got != tc.want {
			t.Errorf("parseContentRangeLength(%q)=%d want %d", tc.in, got, tc.want)
		}
	}
}

func TestIndexOfByte(t *testing.T) {
	if i := indexOfByte("abc", 'b'); i != 1 {
		t.Errorf("got %d", i)
	}
	if i := indexOfByte("abc", 'z'); i != -1 {
		t.Errorf("got %d", i)
	}
	if i := indexOfByte("", 'a'); i != -1 {
		t.Errorf("empty got %d", i)
	}
}

func TestIsRetryable(t *testing.T) {
	retryStatus := []int{429, 500, 502, 503, 504}
	for _, s := range retryStatus {
		if !isRetryable(s, nil) {
			t.Errorf("status %d should be retryable", s)
		}
	}
	nonRetry := []int{200, 400, 403, 404}
	for _, s := range nonRetry {
		if isRetryable(s, nil) {
			t.Errorf("status %d should not be retryable", s)
		}
	}
	// 按 Code 判定
	retryCodes := []string{"SlowDown", "RequestTimeout", "InternalError", "ServiceUnavailable"}
	for _, c := range retryCodes {
		if !isRetryable(200, &ErrorResponse{Code: c}) {
			t.Errorf("code %q should be retryable", c)
		}
	}
	if isRetryable(200, &ErrorResponse{Code: "NoSuchKey"}) {
		t.Error("NoSuchKey not retryable")
	}
}

func TestRetryBackoff(t *testing.T) {
	for i := 0; i < 12; i++ {
		d := retryBackoff(i)
		// d 应在合理范围; 只断言上界 (cap = 10s) 与非负
		if d > 10*time.Second {
			t.Errorf("attempt %d: %v exceeds cap", i, d)
		}
		if d < 0 {
			t.Errorf("negative backoff")
		}
	}
}

func TestS3EncodeQuery(t *testing.T) {
	v := url.Values{}
	v.Add("b", "2")
	v.Add("a", "1")
	v.Add("a", "10")
	got := s3EncodeQuery(v)
	// 键排序: a 在 b 前
	if !strings.HasPrefix(got, "a=") {
		t.Errorf("not sorted by key: %q", got)
	}
	// 特殊字符编码
	v2 := url.Values{}
	v2.Add("k", "a b")
	got2 := s3EncodeQuery(v2)
	if !strings.Contains(got2, "%20") {
		t.Errorf("space not encoded: %q", got2)
	}
	if s3EncodeQuery(nil) != "" {
		t.Error("empty query should be empty string")
	}
}
