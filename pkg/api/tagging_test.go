package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestGetBucketTaggingNoSuchTagSetIsEmpty 回归: 无标签的桶返回 404
// NoSuchTagSet, 这是"空集合"而不是失败; 否则 `tag list` 在干净桶上
// 会以退出码 4 结束。
func TestGetBucketTaggingNoSuchTagSetIsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("tagging") {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`<Error><Code>NoSuchTagSet</Code><Message>The TagSet does not exist</Message></Error>`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c, err := New(&Options{Endpoint: srv.URL, AccessKey: "a", SecretKey: "s", MaxRetries: 1})
	if err != nil {
		t.Fatal(err)
	}
	tags, err := c.GetBucketTagging(context.Background(), "bucket")
	if err != nil {
		t.Fatalf("NoSuchTagSet should be an empty set, got error: %v", err)
	}
	if len(tags) != 0 {
		t.Fatalf("tags = %v, want empty", tags)
	}
}

// TestGetObjectTaggingNoSuchTagSetIsEmpty 部分兼容实现在对象无标签时也回 404。
func TestGetObjectTaggingNoSuchTagSetIsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`<Error><Code>NoSuchTagSet</Code><Message>The TagSet does not exist</Message></Error>`))
	}))
	defer srv.Close()

	c, err := New(&Options{Endpoint: srv.URL, AccessKey: "a", SecretKey: "s", MaxRetries: 1})
	if err != nil {
		t.Fatal(err)
	}
	tags, err := c.GetObjectTagging(context.Background(), "bucket", "key", "")
	if err != nil {
		t.Fatalf("NoSuchTagSet should be an empty set, got error: %v", err)
	}
	if len(tags) != 0 {
		t.Fatalf("tags = %v, want empty", tags)
	}
}
