//go:build aws

// aws_lookup_test.go 覆盖官方 SDK 后端的自定义 bucket 寻址 (templateEndpointResolver):
// 占位符替换 / path 与 host 模板 / %(region) 探测与缓存 / 空 bucket 报错.

package client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func testResolverOpts(endpoint string) s3.Options {
	return s3.Options{
		BaseEndpoint: aws.String(endpoint),
		Region:       "us-east-1",
		Credentials:  credentials.NewStaticCredentialsProvider("access", "secret", ""),
	}
}

func TestTemplateEndpointResolverHostAndPath(t *testing.T) {
	cases := []struct {
		name    string
		tpl     string
		bucket  string
		wantURL string
	}{
		{
			name:    "bucket-in-host",
			tpl:     "http://%(bucket).s3.example.test",
			bucket:  "mybucket",
			wantURL: "http://mybucket.s3.example.test",
		},
		{
			name:    "bucket-in-path",
			tpl:     "http://s3.example.test/data/%(bucket)",
			bucket:  "mybucket",
			wantURL: "http://s3.example.test/data/mybucket",
		},
		{
			name:    "region-in-host",
			tpl:     "http://s3.%(region).example.test",
			bucket:  "mybucket",
			wantURL: "http://s3.us-east-1.example.test",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newTemplateEndpointResolver(c.tpl, "%(bucket)", "%(region)", "us-east-1", testResolverOpts("http://base.example.test"))
			ep, err := r.ResolveEndpoint(context.Background(), s3.EndpointParameters{
				Bucket: aws.String(c.bucket),
				Region: aws.String("us-east-1"),
			})
			if err != nil {
				t.Fatal(err)
			}
			if got := ep.URI.String(); got != c.wantURL {
				t.Fatalf("URI = %q, want %q", got, c.wantURL)
			}
		})
	}
}

func TestTemplateEndpointResolverEmptyBucket(t *testing.T) {
	r := newTemplateEndpointResolver("http://%(bucket).s3.example.test", "%(bucket)", "", "us-east-1", testResolverOpts("http://base.example.test"))
	if _, err := r.ResolveEndpoint(context.Background(), s3.EndpointParameters{}); err == nil {
		t.Fatal("expected error for empty bucket")
	}
}

// TestTemplateEndpointResolverRegionProbe 验证 %(region) 模板会通过 GetBucketLocation
// 探测 bucket 实际 region (只探测一次, 带缓存), 并把探测结果注入模板.
func TestTemplateEndpointResolverRegionProbe(t *testing.T) {
	var probes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !r.URL.Query().Has("location") {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		probes.Add(1)
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/">cn-north-1</LocationConstraint>`))
	}))
	defer server.Close()

	r := newTemplateEndpointResolver("http://%(bucket).s3.%(region).example.test", "%(bucket)", "%(region)", "us-east-1", testResolverOpts(server.URL))

	params := func() s3.EndpointParameters {
		return s3.EndpointParameters{Bucket: aws.String("mybucket"), Region: aws.String("us-east-1")}
	}

	ep, err := r.ResolveEndpoint(context.Background(), params())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := ep.URI.String(), "http://mybucket.s3.cn-north-1.example.test"; got != want {
		t.Fatalf("URI = %q, want %q", got, want)
	}
	if got := probes.Load(); got != 1 {
		t.Fatalf("probes = %d, want 1", got)
	}

	// 第二次解析命中缓存, 不再探测
	ep, err = r.ResolveEndpoint(context.Background(), params())
	if err != nil {
		t.Fatal(err)
	}
	if got := probes.Load(); got != 1 {
		t.Fatalf("probes after cache = %d, want 1", got)
	}
	if got, want := ep.URI.String(), "http://mybucket.s3.cn-north-1.example.test"; got != want {
		t.Fatalf("URI = %q, want %q", got, want)
	}
}

// TestTemplateEndpointResolverRegionProbeFallback 探测失败或返回空 region 时回退配置 region.
func TestTemplateEndpointResolverRegionProbeFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/"></LocationConstraint>`))
	}))
	defer server.Close()

	r := newTemplateEndpointResolver("http://s3.%(region).example.test", "%(bucket)", "%(region)", "us-east-1", testResolverOpts(server.URL))
	ep, err := r.ResolveEndpoint(context.Background(), s3.EndpointParameters{Bucket: aws.String("mybucket"), Region: aws.String("us-east-1")})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := ep.URI.String(), "http://s3.us-east-1.example.test"; got != want {
		t.Fatalf("URI = %q, want %q", got, want)
	}
}

// TestAWSCustomLookupEndToEnd 用模板解析器构建完整 SDK 客户端发请求, 验证最终 URL:
// 自定义模板 (bucket 在 path 中) 经 SDK serialize/finalize 后得到正确的目标地址.
func TestAWSCustomLookupEndToEnd(t *testing.T) {
	var got *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Clone(r.Context())
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Name>mybucket</Name></ListBucketResult>`))
	}))
	defer server.Close()

	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	r := newTemplateEndpointResolver(fmt.Sprintf("http://127.0.0.1:%s/data/%%(bucket)", u.Port()), "%(bucket)", "", "us-east-1", testResolverOpts(server.URL))
	client := s3.New(s3.Options{
		EndpointResolverV2: r,
		Region:             "us-east-1",
		Credentials:        credentials.NewStaticCredentialsProvider("access", "secret", ""),
		HTTPClient:         server.Client(),
	})

	if _, err := client.GetObject(context.Background(), &s3.GetObjectInput{Bucket: aws.String("mybucket"), Key: aws.String("dir/key.txt")}); err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("no request received")
	}
	want := "/data/mybucket/dir/key.txt"
	if got.URL.Path != want {
		t.Fatalf("request path = %q, want %q", got.URL.Path, want)
	}
}
