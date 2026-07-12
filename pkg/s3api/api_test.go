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

func TestPresignedURLRejectsInvalidInputs(t *testing.T) {
	c, err := New(&Options{Endpoint: "https://s3.example.test", AccessKey: "key", SecretKey: "secret", NotCheckVendor: true})
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
	c, err := New(&Options{Endpoint: "https://s3.example.test", AccessKey: "key", SecretKey: "secret", NotCheckVendor: true})
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

func TestProviderCapabilities(t *testing.T) {
	if !((&Client{vendor: ProviderMinIO}).Capabilities().SupportsBucketQuota) {
		t.Fatal("MinIO profile should expose bucket quota support")
	}
	if !((&Client{vendor: Provider("unknown")}).Capabilities().SupportsSigV4) {
		t.Fatal("unknown provider should retain conservative SigV4 compatibility")
	}
}
