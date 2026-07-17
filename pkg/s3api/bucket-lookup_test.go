package s3api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

// testRegionLookup 是测试用的自定义寻址实现, 模板含 %(bucket) 与可选的 %(region)。
type testRegionLookup struct {
	template   string
	needRegion bool
}

func (t *testRegionLookup) NeedsRegion() bool { return t.needRegion }

func (t *testRegionLookup) ResolveCustomEndpoint(bucket, region string) (*url.URL, error) {
	raw := strings.ReplaceAll(t.template, "%(bucket)", bucket)
	raw = strings.ReplaceAll(raw, "%(region)", region)
	return url.Parse(raw)
}

// TestCustomLookupResolvesRegionAndCaches 验证:
//  1. 模板含 %(region) 时, 首次请求通过 GetBucketLocation 探测 region 并注入模板;
//  2. region 被正确拼进最终 URL;
//  3. 后续请求命中缓存, 不再探测 (每 bucket 至多一次探针)。
func TestCustomLookupResolvesRegionAndCaches(t *testing.T) {
	var locationHits atomic.Int32
	var lastObjectPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("location") {
			locationHits.Add(1)
			_, _ = w.Write([]byte(`<LocationConstraint>eu-west-1</LocationConstraint>`))
			return
		}
		lastObjectPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c, err := New(&Options{
		Endpoint:           server.URL,
		AccessKey:          "ak",
		SecretKey:          "sk",
		Region:             "us-east-1",
		BucketLookupViaURL: &testRegionLookup{template: server.URL + "/%(region)/%(bucket)", needRegion: true},
	})
	if err != nil {
		t.Fatal(err)
	}

	doGet := func() {
		resp, err := c.Do(context.Background(), http.MethodGet, requestMetadata{bucketName: "mybucket", objectName: "file.txt"})
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
	}

	doGet() // 首次: 触发一次 ?location 探测
	doGet() // 二次: 命中缓存, 不再探测

	if got := locationHits.Load(); got != 1 {
		t.Fatalf("expected exactly 1 location probe (cached after first), got %d", got)
	}
	if !strings.Contains(lastObjectPath, "eu-west-1") {
		t.Fatalf("region not substituted into URL path: %q", lastObjectPath)
	}
	if !strings.Contains(lastObjectPath, "mybucket") {
		t.Fatalf("bucket missing from URL path: %q", lastObjectPath)
	}
}

// TestCustomLookupWithoutRegionSkipsProbe 验证模板不含 %(region) 时走默认方案, 不做任何探测。
func TestCustomLookupWithoutRegionSkipsProbe(t *testing.T) {
	var locationHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("location") {
			locationHits.Add(1)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c, err := New(&Options{
		Endpoint:           server.URL,
		AccessKey:          "ak",
		SecretKey:          "sk",
		Region:             "us-east-1",
		BucketLookupViaURL: &testRegionLookup{template: server.URL + "/%(bucket)", needRegion: false},
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := c.Do(context.Background(), http.MethodGet, requestMetadata{bucketName: "mybucket", objectName: "file.txt"})
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	if got := locationHits.Load(); got != 0 {
		t.Fatalf("expected no location probe without %%(region), got %d", got)
	}
}
