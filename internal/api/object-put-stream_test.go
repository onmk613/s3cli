// object-put-stream_test.go 覆盖流式上传 (PutObjectStream) 的两个关键契约:
//
//  1. body 的所有权属于调用方 —— http.Transport 在每次尝试后都会 Close req.Body,
//     若 body 被真的关闭, 重试与 region 重定向的 Seek 回卷会以 "file already
//     closed" 失败 (流式上传因此既无重试也无跨 region 失败转移)。
//  2. contentLength 与 body 实际长度不一致时必须报错, 而不是静默上传 0 字节。

package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// tempSeeker 返回一个真实的 *os.File (而非 bytes.Reader): bytes.Reader 的 Close
// 是空操作, 无法暴露 "body 被 Transport 关闭" 这一类缺陷。
func tempSeeker(t *testing.T, n int) (*os.File, int64) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "payload.bin")
	if err := os.WriteFile(p, bytes.Repeat([]byte("x"), n), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	fi, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	return f, fi.Size()
}

// TestPutObjectStreamRetriesAfterServerError 断言 5xx 后会对文件 body 重试,
// 且重试时 body 仍可读 (内容完整送达)。
func TestPutObjectStreamRetriesAfterServerError(t *testing.T) {
	var calls int32
	var retryBody []byte
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`<Error><Code>InternalError</Code><Message>boom</Message></Error>`))
			return
		}
		retryBody = b
		w.Header().Set("ETag", `"ok"`)
		w.WriteHeader(http.StatusOK)
	}))
	defer s.Close()

	c, err := New(&Options{Endpoint: s.URL, AccessKey: "ak", SecretKey: "sk", BucketLookup: BucketLookupPath, MaxRetries: 3})
	if err != nil {
		t.Fatal(err)
	}

	f, size := tempSeeker(t, 4096)
	out, err := c.PutObjectStream(context.Background(), "bkt", "k", f, size, nil)
	if err != nil {
		t.Fatalf("PutObjectStream should retry on 5xx, got %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("want 2 server calls (1 fail + 1 retry), got %d", got)
	}
	if int64(len(retryBody)) != size {
		t.Fatalf("retried body truncated: got %d bytes, want %d", len(retryBody), size)
	}
	if out.ETag != "ok" {
		t.Fatalf("want ETag ok, got %q", out.ETag)
	}
}

// TestPutObjectStreamRegionRedirect 断言 301 + X-Amz-Bucket-Region 时能重签重发,
// 而不是把 "file already closed" 暴露给用户。
func TestPutObjectStreamRegionRedirect(t *testing.T) {
	var calls int32
	var gotAuth string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.Header().Set("X-Amz-Bucket-Region", "eu-west-1")
			w.WriteHeader(http.StatusMovedPermanently)
			return
		}
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer s.Close()

	c, err := New(&Options{
		Endpoint: s.URL, AccessKey: "ak", SecretKey: "sk",
		BucketLookup: BucketLookupPath, Region: "us-east-1", MaxRetries: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	f, size := tempSeeker(t, 1024)
	if _, err := c.PutObjectStream(context.Background(), "bkt", "k", f, size, nil); err != nil {
		t.Fatalf("region redirect must re-sign and resend, got %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("want 2 server calls, got %d", got)
	}
	if !bytes.Contains([]byte(gotAuth), []byte("/eu-west-1/s3/aws4_request")) {
		t.Fatalf("retry must be signed for the redirected region, got %q", gotAuth)
	}
}

// TestPutObjectStreamKeepsCallerBodyOpen 断言 API 不关闭调用方的 body。
func TestPutObjectStreamKeepsCallerBodyOpen(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer s.Close()

	c, err := New(&Options{Endpoint: s.URL, AccessKey: "ak", SecretKey: "sk", BucketLookup: BucketLookupPath})
	if err != nil {
		t.Fatal(err)
	}
	f, size := tempSeeker(t, 512)
	if _, err := c.PutObjectStream(context.Background(), "bkt", "k", f, size, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("body must stay open and seekable after the call: %v", err)
	}
}

// TestPutObjectStreamRejectsWrongContentLength 断言长度不一致不会被静默截断成 0 字节。
func TestPutObjectStreamRejectsWrongContentLength(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, _ := io.Copy(io.Discard, r.Body)
		if n != 0 {
			t.Errorf("no request should carry a body for the mismatch case, got %d bytes", n)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer s.Close()

	c, err := New(&Options{Endpoint: s.URL, AccessKey: "ak", SecretKey: "sk", BucketLookup: BucketLookupPath})
	if err != nil {
		t.Fatal(err)
	}
	f, _ := tempSeeker(t, 4096)
	_, err = c.PutObjectStream(context.Background(), "bkt", "k", f, 0, nil)
	if err == nil {
		t.Fatal("contentLength=0 with a non-empty body must be rejected, not silently uploaded as empty")
	}
	if want := "does not match body length"; !bytes.Contains([]byte(err.Error()), []byte(want)) {
		t.Fatalf("want error mentioning %q, got %v", want, err)
	}
}

// TestPutObjectStreamEmptyBodyAllowed 断言真正的空 body 仍然可用。
func TestPutObjectStreamEmptyBodyAllowed(t *testing.T) {
	var n int64 = -1
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer s.Close()

	c, err := New(&Options{Endpoint: s.URL, AccessKey: "ak", SecretKey: "sk", BucketLookup: BucketLookupPath})
	if err != nil {
		t.Fatal(err)
	}
	f, size := tempSeeker(t, 0)
	if _, err := c.PutObjectStream(context.Background(), "bkt", "k", f, size, nil); err != nil {
		t.Fatalf("empty object upload must succeed, got %v", err)
	}
	if n != 0 {
		t.Fatalf("want 0 body bytes, got %d", n)
	}
}
