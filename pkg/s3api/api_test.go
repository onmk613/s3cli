package s3api

import (
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestBuildURLEscapesObjectKeyExactlyOnce(t *testing.T) {
	c := &Client{}
	u, err := c.buildURL("s3.example.test", "https", "bucket", "dir/a b%2Fc+中文.txt", nil, BucketLookupPath)
	if err != nil {
		t.Fatal(err)
	}
	got := u.String()
	for _, want := range []string{"a%20b%252Fc%2B", "%E4%B8%AD%E6%96%87"} {
		if !strings.Contains(got, want) {
			t.Fatalf("URL %q does not contain %q", got, want)
		}
	}
	if strings.Contains(got, "%252520") {
		t.Fatalf("URL double-encoded percent escape: %q", got)
	}
	if decoded, err := url.PathUnescape(u.EscapedPath()); err != nil || decoded != "/bucket/dir/a b%2Fc+中文.txt" {
		t.Fatalf("escaped path did not round trip: %q, %v", decoded, err)
	}
}

// TestBuildURLFromBaseNoDoubleBucket 验证自定义寻址不会把 bucket 同时放进 host 和 path。
// 模板解析出的 base 已含 bucket, 此处仅应追加 object key。
func TestBuildURLFromBaseNoDoubleBucket(t *testing.T) {
	// 虚拟主机模板: https://www.%(bucket).example.com -> bucket 已在 host
	vhostBase := mustParseURL(t, "https://www.mydata.example.com")
	u, err := buildURLFromBase(vhostBase, "photos/cat.png", nil)
	if err != nil {
		t.Fatal(err)
	}
	// 期望 host 保留 bucket, path 仅含 object, 不重复出现 bucket
	if got := u.String(); got != "https://www.mydata.example.com/photos/cat.png" {
		t.Fatalf("vhost custom URL = %q, want no double bucket", got)
	}
	if strings.Count(u.Path, "mydata") != 0 {
		t.Fatalf("bucket leaked into path: %q", u.Path)
	}

	// 无 object (bucket 根级请求): 虚拟主机应为 https://www.mydata.example.com/
	u2, err := buildURLFromBase(vhostBase, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := u2.String(); got != "https://www.mydata.example.com/" {
		t.Fatalf("bucket-root custom URL = %q", got)
	}

	// path 风格模板: https://example.com/%(bucket) -> base path=/mydata
	pathBase := mustParseURL(t, "https://example.com/mydata")
	u3, err := buildURLFromBase(pathBase, "photos/cat.png", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := u3.String(); got != "https://example.com/mydata/photos/cat.png" {
		t.Fatalf("path-style custom URL = %q", got)
	}

	// object key 中的特殊字符需按 SigV4 转义一次
	u4, err := buildURLFromBase(vhostBase, "a b/中文", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(u4.String(), "a%20b/") || !strings.Contains(u4.RawPath, "%E4%B8%AD%E6%96%87") {
		t.Fatalf("object key not escaped once: %q (rawpath %q)", u4.String(), u4.RawPath)
	}
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u
}

func TestPresignedURLRejectsInvalidInputs(t *testing.T) {
	c, err := New(&Options{Endpoint: "https://s3.example.test", AccessKey: "key", SecretKey: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.PresignedURL(nil, "bucket", "key", &PresignOptions{Method: "POST"}); err == nil {
		t.Fatal("expected unsupported method error")
	}
	if _, err := c.PresignedURL(nil, "bucket", "key", &PresignOptions{Expires: 7*24*time.Hour + time.Second}); err == nil {
		t.Fatal("expected maximum expiry error")
	}
}

func TestPresignV2UsesBase64SHA1Signature(t *testing.T) {
	c, err := New(&Options{Endpoint: "https://s3.example.test", AccessKey: "key", SecretKey: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	signed, err := c.PresignV2(nil, "bucket", "object", "GET", 60)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(signed)
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Get("AWSAccessKeyId") != "key" {
		t.Fatalf("access key missing from %q", signed)
	}
	mac, err := base64.StdEncoding.DecodeString(u.Query().Get("Signature"))
	if err != nil || len(mac) != 20 {
		t.Fatalf("signature is not HMAC-SHA1 Base64: %q (%v)", u.Query().Get("Signature"), err)
	}
	if _, err := c.PresignV2(nil, "bucket", "object", "POST", 60); err == nil {
		t.Fatal("expected unsupported method error")
	}
}
