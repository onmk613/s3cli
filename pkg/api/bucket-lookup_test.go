package api

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

// TestCustomLookupRejectsInvalidBucketAtRuntime 验证自定义模板在运行期对真实 bucket 名做
// 二次校验: 配置期校验只用固定测试值 ("test-bucket"), 含 ".." 的 bucket 名替换进 host
// 模板后会拼出非法 host, 必须在发请求前返回带 bucket 名信息的错误。
func TestCustomLookupRejectsInvalidBucketAtRuntime(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c, err := New(&Options{
		Endpoint:           server.URL,
		AccessKey:          "ak",
		SecretKey:          "sk",
		BucketLookupViaURL: &testRegionLookup{template: "https://%(bucket).example.com", needRegion: false},
	})
	if err != nil {
		t.Fatal(err)
	}

	// 非法 bucket: host 模板替换后含 "..", 必须报错且错误信息含 bucket 名与
	// 模板输出 (断言校验文案, 避免依赖 DNS 解析错误, 证明错误来自运行期校验)
	_, err = c.Do(context.Background(), http.MethodGet, requestMetadata{bucketName: "a..b", objectName: "key"})
	if err == nil {
		t.Fatal("expected error for bucket 'a..b' substituted into host template")
	}
	if !strings.Contains(err.Error(), "a..b") {
		t.Fatalf("error should mention bucket name, got: %v", err)
	}
	if !strings.Contains(err.Error(), `must not contain ".."`) {
		t.Fatalf("error should come from runtime validation, got: %v", err)
	}

	// 对照: 同样的 bucket 放进 path 风格模板 (host 不含 "..") 不受影响, 请求正常发出
	c2, err := New(&Options{
		Endpoint:           server.URL,
		AccessKey:          "ak",
		SecretKey:          "sk",
		BucketLookupViaURL: &testRegionLookup{template: server.URL + "/%(bucket)", needRegion: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c2.Do(context.Background(), http.MethodGet, requestMetadata{bucketName: "a..b", objectName: "key"})
	if err != nil {
		t.Fatalf("path-style template should accept bucket 'a..b': %v", err)
	}
	_ = resp.Body.Close()
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
