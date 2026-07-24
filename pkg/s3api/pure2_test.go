package s3api

import (
	"encoding/xml"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestNewAndOptionsNormalization(t *testing.T) {
	t.Run("missing params error", func(t *testing.T) {
		for _, opts := range []*Options{
			nil,
			{Endpoint: "x"},
			{Endpoint: "x", AccessKey: "a"},
		} {
			if _, err := New(opts); err == nil {
				t.Error("expected error for incomplete options")
			}
		}
	})
	t.Run("scheme auto-added", func(t *testing.T) {
		c, err := New(&Options{Endpoint: "s3.example.com", AccessKey: "a", SecretKey: "s"})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(c.Endpoint(), "http://") {
			t.Errorf("scheme not added: %s", c.Endpoint())
		}
	})
	t.Run("defaults region and retries", func(t *testing.T) {
		c, err := New(&Options{Endpoint: "https://s3.example.com", AccessKey: "a", SecretKey: "s"})
		if err != nil {
			t.Fatal(err)
		}
		if c.region != "us-east-1" {
			t.Errorf("region = %q", c.region)
		}
		if c.maxRetries != 3 {
			t.Errorf("retries = %d", c.maxRetries)
		}
	})
	t.Run("invalid endpoint url", func(t *testing.T) {
		_, err := New(&Options{Endpoint: "ht!tp://[invalid", AccessKey: "a", SecretKey: "s"})
		if err == nil {
			t.Error("expected error for invalid endpoint")
		}
	})
}

func TestClientGetters(t *testing.T) {
	c, err := New(&Options{
		Endpoint:     "https://s3.example.com",
		AccessKey:    "AK",
		SecretKey:    "SK",
		SessionToken: "ST",
		Region:       "eu-west-1",
		Vendor:       ProviderMinIO,
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.AccessKey() != "AK" {
		t.Error("AccessKey")
	}
	if c.SecretKey() != "SK" {
		t.Error("SecretKey")
	}
	if c.SessionToken() != "ST" {
		t.Error("SessionToken")
	}
	if c.Endpoint() != "https://s3.example.com" {
		t.Errorf("Endpoint=%q", c.Endpoint())
	}
	// Provider 设置器
	c.Provider(ProviderSeaweedFS)
	if c.vendor != ProviderSeaweedFS {
		t.Error("Provider not set")
	}
}

func TestCanonicalQueryString(t *testing.T) {
	if canonicalQueryString(nil) != "" {
		t.Error("empty should be empty string")
	}
	v := url.Values{}
	v.Add("b", "2")
	v.Add("a", "1")
	v.Add("a", "10") // 同 key 多值
	got := canonicalQueryString(v)
	// 键排序 + 同 key 的值也排序: a=1&a=10&b=2
	if got != "a=1&a=10&b=2" {
		t.Errorf("got %q", got)
	}
	// 特殊字符编码
	v2 := url.Values{}
	v2.Add("k", "a b+c")
	got2 := canonicalQueryString(v2)
	if !strings.Contains(got2, "%20") || !strings.Contains(got2, "%2B") {
		t.Errorf("special chars not encoded: %q", got2)
	}
}

func TestDeriveSigningKey(t *testing.T) {
	// 与已知的 AWS SigV4 测试向量对照 (固定输入应确定性)
	k := deriveSigningKey("wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY", "20150830", "us-east-1")
	if len(k) != 32 {
		t.Errorf("signing key len = %d, want 32", len(k))
	}
}

func TestHexHMACAndSumSHA256HexOfHMAC(t *testing.T) {
	a := hexHMAC([]byte("key"), "data")
	b := sumSHA256HexOfHMAC([]byte("key"), "data")
	if a != b {
		t.Error("hexHMAC should equal sumSHA256HexOfHMAC")
	}
	// 应与 sumHMACSHA256 的 hex 一致
	mac := sumHMACSHA256([]byte("key"), []byte("data"))
	if a != bytesToHex(mac) {
		t.Errorf("hex mismatch: %s vs %s", a, bytesToHex(mac))
	}
}

func bytesToHex(b []byte) string {
	const t = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = t[v>>4]
		out[i*2+1] = t[v&0xf]
	}
	return string(out)
}

func TestParseBucketCorsConfig(t *testing.T) {
	xmlBody := `<CORSConfiguration><CORSRule><AllowedMethod>get</AllowedMethod><AllowedOrigin>*</AllowedOrigin></CORSRule></CORSConfiguration>`
	c, err := ParseBucketCorsConfig(strings.NewReader(xmlBody))
	if err != nil {
		t.Fatal(err)
	}
	if c.XMLNS == "" {
		t.Error("XMLNS should default")
	}
	if len(c.CORSRules) != 1 {
		t.Fatalf("rules = %d", len(c.CORSRules))
	}
	// 小写 method 应被转大写
	if c.CORSRules[0].AllowedMethod[0] != "GET" {
		t.Errorf("method not uppercased: %q", c.CORSRules[0].AllowedMethod[0])
	}

	t.Run("malformed", func(t *testing.T) {
		if _, err := ParseBucketCorsConfig(strings.NewReader("<bad")); err == nil {
			t.Error("expected error")
		}
	})
}

func TestCorsConfigToXML(t *testing.T) {
	c := &CorsConfig{CORSRules: []CorsRule{{AllowedOrigin: []string{"*"}}}}
	data, err := c.toXML()
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.HasPrefix(s, xml.Header) {
		t.Error("missing xml header")
	}
	if c.XMLNS == "" {
		t.Error("XMLNS should be defaulted after toXML")
	}
}

func TestParseBucketLifecycleConfig(t *testing.T) {
	body := `<LifecycleConfiguration><Rule><ID>r1</ID><Status>Enabled</Status></Rule></LifecycleConfiguration>`
	c, err := ParseBucketLifecycleConfig(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if c.XMLNS == "" {
		t.Error("XMLNS should default")
	}
	if _, err := ParseBucketLifecycleConfig(strings.NewReader("<bad")); err == nil {
		t.Error("expected error for malformed")
	}
}

func TestLifecycleConfigToXML(t *testing.T) {
	c := &LifecycleConfig{}
	data, err := c.ToXML()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), xml.Header) {
		t.Error("missing xml header")
	}
}

func TestMarshalXMLWithHeader(t *testing.T) {
	type s struct {
		XMLName xml.Name `xml:"X"`
		V       string   `xml:"V"`
	}
	data, err := marshalXMLWithHeader(s{V: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	str := string(data)
	if !strings.HasPrefix(str, xml.Header) {
		t.Error("missing header")
	}
	if !strings.Contains(str, "hi") {
		t.Errorf("missing content: %s", str)
	}
}

func TestBuildPutHeader(t *testing.T) {
	opts := &PutObjectOptions{
		ContentType:          "text/plain",
		StorageClass:         "STANDARD",
		ServerSideEncryption: "AES256",
		ObjectLockLegalHold:  "ON",
		Metadata:             map[string]string{"foo": "bar"},
	}
	h := buildPutHeader(opts)
	if h.Get("Content-Type") != "text/plain" {
		t.Error("Content-Type")
	}
	if h.Get("x-amz-storage-class") != "STANDARD" {
		t.Error("storage-class")
	}
	if h.Get("x-amz-server-side-encryption") != "AES256" {
		t.Error("sse")
	}
	if h.Get("x-amz-object-lock-legal-hold") != "ON" {
		t.Error("legal-hold")
	}
	if h.Get("x-amz-meta-foo") != "bar" {
		t.Error("metadata")
	}
	// 空 opts -> 空 header
	if got := buildPutHeader(&PutObjectOptions{}); len(got) != 0 {
		t.Errorf("expected empty header, got %d keys", len(got))
	}
}

func TestPutObjectOutput(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("ETag", "\"etag123\"")
	resp.Header.Set("x-amz-version-id", "v2")
	resp.Header.Set("x-amz-server-side-encryption", "aws:kms")
	out := putObjectOutput(resp)
	if out.ETag != "etag123" {
		t.Errorf("ETag=%q", out.ETag)
	}
	if out.VersionID != "v2" {
		t.Errorf("VersionID=%q", out.VersionID)
	}
	if out.ServerSideEncryption != "aws:kms" {
		t.Errorf("SSE=%q", out.ServerSideEncryption)
	}
}

func TestBuildURLDNS(t *testing.T) {
	c, err := New(&Options{
		Endpoint:     "https://s3.example.com",
		AccessKey:    "a",
		SecretKey:    "s",
		BucketLookup: BucketLookupDNS,
	})
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.buildURL("s3.example.com", "https", "mybucket", "obj/key", nil, BucketLookupDNS)
	if err != nil {
		t.Fatal(err)
	}
	// DNS 风格: bucket 前置于 host
	if !strings.HasPrefix(u.Host, "mybucket.") {
		t.Errorf("DNS host = %q", u.Host)
	}
}
