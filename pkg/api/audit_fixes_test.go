// audit_fixes_test.go — 2026-08 全面审计后的协议层回归测试:
//  1. CreateBucket 非 us-east-1 必须携带 LocationConstraint 请求体; us-east-1 必须为空 body
//  2. 桶级 Object Lock 子资源名为 object-lock (带连字符)
//  3. SigV4 官方测试向量回归 (独立于本仓库代码的权威签名基准)
package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// recordedRequest 捕获一次请求的方法/路径/查询/请求体。
type recordedRequest struct {
	Method string
	Path   string
	Query  string
	Body   string
}

func newRequestRecorder() (*[]recordedRequest, *sync.Mutex, *httptest.Server) {
	var mu sync.Mutex
	var reqs []recordedRequest
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		reqs = append(reqs, recordedRequest{Method: r.Method, Path: r.URL.EscapedPath(), Query: r.URL.RawQuery, Body: string(body)})
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	return &reqs, &mu, s
}

func TestCreateBucketSendsLocationConstraint(t *testing.T) {
	reqs, mu, s := newRequestRecorder()
	defer s.Close()
	c, err := New(&Options{Endpoint: s.URL, AccessKey: "a", SecretKey: "b"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if err := c.CreateBucket(ctx, "bkt-eu", &MakeBucketOptions{Region: "eu-west-1"}); err != nil {
		t.Fatal(err)
	}
	if err := c.CreateBucket(ctx, "bkt-use1", &MakeBucketOptions{Region: "us-east-1"}); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(*reqs) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(*reqs))
	}
	eu, use1 := (*reqs)[0], (*reqs)[1]
	if !strings.Contains(eu.Body, "<LocationConstraint>eu-west-1</LocationConstraint>") {
		t.Fatalf("non-us-east-1 CreateBucket must send LocationConstraint body, got: %q", eu.Body)
	}
	if !strings.Contains(eu.Body, `xmlns="http://s3.amazonaws.com/doc/2006-03-01/"`) {
		t.Fatalf("CreateBucketConfiguration must carry the S3 namespace, got: %q", eu.Body)
	}
	if use1.Body != "" {
		t.Fatalf("us-east-1 CreateBucket must send an empty body, got: %q", use1.Body)
	}
}

func TestBucketObjectLockUsesHyphenatedSubresource(t *testing.T) {
	reqs, mu, s := newRequestRecorder()
	defer s.Close()
	c, err := New(&Options{Endpoint: s.URL, AccessKey: "a", SecretKey: "b"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	_, _ = c.GetObjectLockConfiguration(ctx, "bkt")
	_ = c.PutObjectLockConfiguration(ctx, "bkt", &ObjectLockConfiguration{})

	mu.Lock()
	defer mu.Unlock()
	if len(*reqs) < 2 {
		t.Fatalf("expected 2 requests, got %d", len(*reqs))
	}
	for _, r := range *reqs {
		if strings.Contains(r.Query, "objectLock") {
			t.Fatalf("object lock subresource must be 'object-lock' (hyphenated), got query: %q", r.Query)
		}
		if !strings.Contains(r.Query, "object-lock") {
			t.Fatalf("expected object-lock in query, got: %q", r.Query)
		}
	}
}

// TestEndpointPathPrefixPreserved 回归: endpoint 带路径前缀 (如网关
// https://gw.example.com/s3) 时, path/DNS 寻址不得丢掉前缀, 否则全部 404。
func TestEndpointPathPrefixPreserved(t *testing.T) {
	reqs, mu, s := newRequestRecorder()
	defer s.Close()
	// 给 endpoint 追加路径前缀: 复用 recorder 的 host。
	c, err := New(&Options{Endpoint: s.URL + "/s3/prefix", AccessKey: "a", SecretKey: "b"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	_, _ = c.ListBuckets(ctx)                                              // service 级
	_, _ = c.PutObject(ctx, "bkt", "obj", []byte("x"), nil)                // path 风格
	_, _ = c.ListObjectsV2(ctx, "bkt", &ListObjectsV2Options{Prefix: "p"}) // path 风格列举

	mu.Lock()
	if len(*reqs) != 3 {
		mu.Unlock()
		t.Fatalf("expected 3 requests, got %d: %+v", len(*reqs), *reqs)
	}
	for i, want := range []string{
		"/s3/prefix/",        // ListBuckets
		"/s3/prefix/bkt/obj", // PutObject
		"/s3/prefix/bkt",     // ListObjects
	} {
		if got := (*reqs)[i].Path; got != want {
			t.Errorf("request %d path = %q, want %q", i, got, want)
		}
	}
	mu.Unlock()

	// DNS 风格: bucket 进 host, 路径前缀保留在 path (无法用 httptest 实测, 直接单测拼 URL)。
	c2, err := New(&Options{Endpoint: "http://s3.example.test/s3/prefix", AccessKey: "a", SecretKey: "b"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := c2.buildURL("s3.example.test", "http", "/s3/prefix/", "bkt", "obj", nil, BucketLookupDNS)
	if err != nil {
		t.Fatal(err)
	}
	if u.Host != "bkt.s3.example.test" || u.Path != "/s3/prefix/obj" {
		t.Fatalf("DNS buildURL = %s%s, want bkt.s3.example.test/s3/prefix/obj", u.Host, u.Path)
	}
}

// =============== SigV4 官方向量回归 ===============
//
// 此前 SigV4 只有"自证往返"测试 (用同一套实现生成再验证), 无法发现系统性
// 签名错误。以下两个向量分别来自 AWS SigV4 官方测试套件 (get-vanilla) 与
// AWS 开发者文档的经典 S3 签名示例, 期望值独立于本仓库代码。

const (
	vectorAccessKey = "AKIDEXAMPLE"
	vectorSecretKey = "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY"
)

// TestSignV4OfficialGetVanilla AWS SigV4 Test Suite header/get-vanilla:
// GET / 到 example.amazonaws.com, service "service", region us-east-1,
// 期望签名为套件发布值。
func TestSignV4OfficialGetVanilla(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://example.amazonaws.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	signV4Service(req, vectorAccessKey, vectorSecretKey, "us-east-1", "service",
		emptySHA256Hex, time.Date(2015, 8, 30, 12, 36, 0, 0, time.UTC))

	want := "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20150830/us-east-1/service/aws4_request, " +
		"SignedHeaders=host;x-amz-date, " +
		"Signature=5fa00fa31553b73ebf1942676e86291e8372ff2a2260956d9b8aae1d763fbf31"
	if got := req.Header.Get("Authorization"); got != want {
		t.Fatalf("get-vanilla Authorization:\n got %s\nwant %s", got, want)
	}
}

// TestSignV4AWSDocsS3Example AWS 文档 "Authenticating Requests (AWS Signature
// Version 4)" 的示例: GET examplebucket.s3.amazonaws.com/test.txt 带 Range 头,
// service s3, region us-east-1。
func TestSignV4AWSDocsS3Example(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://examplebucket.s3.amazonaws.com/test.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Range", "bytes=0-9")
	// 真实流程中 newRequest 在签名前设置该头 (见 api.go), 官方示例的
	// SignedHeaders 也包含 x-amz-content-sha256, 测试须与之对齐。
	req.Header.Set("X-Amz-Content-Sha256", emptySHA256Hex)
	signV4(req, "AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "us-east-1",
		emptySHA256Hex, time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC))

	want := "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request, " +
		"SignedHeaders=host;range;x-amz-content-sha256;x-amz-date, " +
		"Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"
	if got := req.Header.Get("Authorization"); got != want {
		t.Fatalf("docs example Authorization:\n got %s\nwant %s", got, want)
	}
}
