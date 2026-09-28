package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func testClient(t *testing.T, h http.Handler) *Client {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	c, err := New(&Options{Endpoint: s.URL, AccessKey: "access", SecretKey: "secret", MaxRetries: 2})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestDoRetriesRetryableStatusAndSignsRequest(t *testing.T) {
	var calls atomic.Int32
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" || r.Header.Get("X-Amz-Date") == "" {
			t.Error("request was not signed")
		}
		if calls.Add(1) == 1 {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`<Error><Code>SlowDown</Code></Error>`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	resp, err := c.Do(context.Background(), http.MethodGet, requestMetadata{bucketName: "bucket", objectName: "key"})
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
}

func TestListObjectsV2Paginator(t *testing.T) {
	var requests atomic.Int32
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("list-type"); got != "2" {
			t.Fatalf("list-type = %q", got)
		}
		if requests.Add(1) == 1 {
			_, _ = fmt.Fprint(w, `<ListBucketResult><IsTruncated>true</IsTruncated><NextContinuationToken>next</NextContinuationToken><Contents><Key>a</Key><Size>1</Size></Contents></ListBucketResult>`)
			return
		}
		if got := r.URL.Query().Get("continuation-token"); got != "next" {
			t.Fatalf("token = %q", got)
		}
		_, _ = fmt.Fprint(w, `<ListBucketResult><IsTruncated>false</IsTruncated><Contents><Key>b</Key><Size>2</Size></Contents></ListBucketResult>`)
	}))
	p := NewListObjectsV2Paginator(c, "bucket", &ListObjectsV2Options{})
	var keys []string
	for p.HasMorePages() {
		page, err := p.NextPage(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, page.Contents[0].Key)
	}
	if fmt.Sprint(keys) != "[a b]" {
		t.Fatalf("keys = %v", keys)
	}
}

func TestParseErrorResponseFallback(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusForbidden, Status: "403 Forbidden", Header: http.Header{"X-Amz-Request-Id": {"request"}}, Body: http.NoBody}
	err := parseErrorResponse(resp, "bucket", "key")
	if err.Code != "AccessDenied" || err.RequestID != "request" || err.BucketName != "bucket" {
		t.Fatalf("error = %#v", err)
	}
}

func TestDoRetriesWithRedirectedBucketRegion(t *testing.T) {
	var calls atomic.Int32
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("X-Amz-Bucket-Region", "eu-west-1")
			w.WriteHeader(http.StatusMovedPermanently)
			return
		}
		if !strings.Contains(r.Header.Get("Authorization"), "/eu-west-1/s3/aws4_request") {
			t.Errorf("authorization was not re-signed for redirected region: %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	resp, err := c.Do(context.Background(), http.MethodGet, requestMetadata{bucketName: "bucket"})
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if calls.Load() != 2 {
		t.Fatalf("calls=%d", calls.Load())
	}
}

// TestPutEmptyObjectSendsContentLengthZero 验证 0 字节对象上传发送 Content-Length: 0
// 而非 chunked 编码 (严格服务端会拒绝 chunked 的 0 字节 PUT)。
// 覆盖两条路径: PutObject (空切片) 与 PutObjectStream (0 长度 *os.File,
// 非 Len() reader + contentLength=0 此前会被 transport 视为长度未知而走 chunked)。
func TestPutEmptyObjectSendsContentLengthZero(t *testing.T) {
	var mu sync.Mutex
	var transferEncodings [][]string
	var contentLengths []string
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		transferEncodings = append(transferEncodings, append([]string(nil), r.TransferEncoding...))
		contentLengths = append(contentLengths, r.Header.Get("Content-Length"))
		mu.Unlock()
		w.Header().Set("ETag", `"empty"`)
		w.WriteHeader(http.StatusOK)
	}))

	if _, err := c.PutObject(context.Background(), "bucket", "empty-obj", []byte{}, nil); err != nil {
		t.Fatal(err)
	}

	// 0 长度临时文件: 非 Len() reader, 是此前 chunked 的真实触发路径
	f, err := os.CreateTemp(t.TempDir(), "empty-obj")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := c.PutObjectStream(context.Background(), "bucket", "empty-stream", f, 0, nil); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(transferEncodings) != 2 {
		t.Fatalf("server saw %d requests, want 2", len(transferEncodings))
	}
	for i := range transferEncodings {
		if len(transferEncodings[i]) != 0 {
			t.Errorf("request %d: Transfer-Encoding = %v, want none (no chunked)", i, transferEncodings[i])
		}
		if contentLengths[i] != "0" {
			t.Errorf("request %d: Content-Length = %q, want \"0\"", i, contentLengths[i])
		}
	}
}

// TestPutEmptyObjectRetriesSafely 验证 0 字节 PUT 在可重试错误后仍能重试:
// Do 的回卷判断基于 meta.contentBody (empty bytes.Reader 仍是 ReadSeeker),
// 不被 newRequest 中把 req.Body 替换为 http.NoBody 破坏。
func TestPutEmptyObjectRetriesSafely(t *testing.T) {
	var calls atomic.Int32
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	if _, err := c.PutObject(context.Background(), "bucket", "empty", []byte{}, nil); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("calls = %d, want 2 (retry must still happen)", got)
	}
}

// TestDoDoesNotAutoFollow301AndResignsWithRedirectedRegion 验证 301 响应即使带 Location
// 头也不会被 http.Client 自动跟随 (CheckRedirect=ErrUseLastResponse), Do 收到响应后
// 自行解析 X-Amz-Bucket-Region 重签重发: 第二次请求仍打到原 URL, 且 Authorization
// 已按新 region 重新签名。修复前 http.Client 默认自动跟随, 会尝试访问 Location 指向的
// 外部 host (跨 host 丢失 Authorization), 该用例会直接失败。
func TestDoDoesNotAutoFollow301AndResignsWithRedirectedRegion(t *testing.T) {
	var calls atomic.Int32
	var secondPath, secondAuth string
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("X-Amz-Bucket-Region", "eu-west-1")
			w.Header().Set("Location", "https://bucket.s3.eu-west-1.amazonaws.com/key")
			w.WriteHeader(http.StatusMovedPermanently)
			return
		}
		secondPath = r.URL.Path
		secondAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	resp, err := c.Do(context.Background(), http.MethodGet, requestMetadata{bucketName: "bucket", objectName: "key"})
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if got := calls.Load(); got != 2 {
		t.Fatalf("calls = %d, want 2 (client must not auto-follow redirect)", got)
	}
	if secondPath != "/bucket/key" {
		t.Fatalf("second request path = %q, want original /bucket/key", secondPath)
	}
	if !strings.Contains(secondAuth, "/eu-west-1/s3/aws4_request") {
		t.Fatalf("second request not re-signed for redirected region: %q", secondAuth)
	}
}

// TestDo400WithBucketRegionResigns 验证 400 + X-Amz-Bucket-Region 分支仍生效
// (该分支不依赖重定向, 是 region 纠正的另一条路径), 且 region 会写入缓存。
func TestDo400WithBucketRegionResigns(t *testing.T) {
	var calls atomic.Int32
	var secondAuth string
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("X-Amz-Bucket-Region", "ap-southeast-1")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(w, `<Error><Code>AuthorizationHeaderMalformed</Code></Error>`)
			return
		}
		secondAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	resp, err := c.Do(context.Background(), http.MethodGet, requestMetadata{bucketName: "bucket"})
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if got := calls.Load(); got != 2 {
		t.Fatalf("calls = %d, want 2", got)
	}
	if !strings.Contains(secondAuth, "/ap-southeast-1/s3/aws4_request") {
		t.Fatalf("second request not re-signed for region: %q", secondAuth)
	}
	if v, ok := c.bucketLocCache.Get("bucket"); !ok || v != "ap-southeast-1" {
		t.Fatalf("bucket region not cached: %q, %v", v, ok)
	}
}

// TestCopyObjectDetectsEmbeddedError 验证 CopyObject 对 "200 + body 内嵌 <Error>"
// 的响应返回错误而非误判成功; 同时验证正常 200 响应不受影响。
func TestCopyObjectDetectsEmbeddedError(t *testing.T) {
	var embedded atomic.Bool
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-amz-copy-source") == "" {
			t.Error("missing x-amz-copy-source header")
		}
		w.WriteHeader(http.StatusOK)
		if embedded.Load() {
			_, _ = fmt.Fprint(w, `<Error><Code>InternalError</Code><Message>copy failed midway</Message></Error>`)
			return
		}
		_, _ = fmt.Fprint(w, `<CopyObjectResult><ETag>"etag-1"</ETag><LastModified>2026-01-01T00:00:00Z</LastModified></CopyObjectResult>`)
	}))

	// 正常 200: 成功
	out, err := c.CopyObject(context.Background(), "src-bucket", "src/key", "dst-bucket", "dst/key", nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.ETag != "etag-1" {
		t.Fatalf("ETag = %q", out.ETag)
	}

	// 200 内嵌 <Error>: 必须报错
	embedded.Store(true)
	_, err = c.CopyObject(context.Background(), "src-bucket", "src/key", "dst-bucket", "dst/key", nil)
	if err == nil {
		t.Fatal("embedded <Error> in 200 response was treated as success")
	}
	var apiErr *ErrorResponse
	if !errors.As(err, &apiErr) || apiErr.Code != "InternalError" {
		t.Fatalf("expected *ErrorResponse(InternalError), got %v", err)
	}
}

// TestCopyObjectEncodesSourceVersionID 验证 versionId 在 x-amz-copy-source 中被 percent-encode。
func TestCopyObjectEncodesSourceVersionID(t *testing.T) {
	var gotSource string
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSource = r.Header.Get("x-amz-copy-source")
		_, _ = fmt.Fprint(w, `<CopyObjectResult><ETag>"e"</ETag></CopyObjectResult>`)
	}))
	if _, err := c.CopyObject(context.Background(), "b", "k", "d", "dk", &CopyObjectOptions{SourceVersionID: "a/b+c="}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotSource, "versionId=a%2Fb%2Bc%3D") {
		t.Fatalf("versionId not percent-encoded: %q", gotSource)
	}
}
