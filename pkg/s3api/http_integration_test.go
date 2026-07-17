package s3api

import (
	"context"
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
