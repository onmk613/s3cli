package s3api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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
