package client

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"s3cli/internal/config"
	"s3cli/internal/s3path"
	"s3cli/pkg/kvcache"
	"s3cli/pkg/s3api"
)

func TestParseProvider(t *testing.T) {
	cases := []struct {
		in   string
		want s3api.Provider
	}{
		{"", s3api.ProviderAws},
		{"aws", s3api.ProviderAws},
		{"AWS", s3api.ProviderAws},
		{" minio ", s3api.ProviderMinIO},
		{"MINIO", s3api.ProviderMinIO},
		{"seaweedfs", s3api.ProviderSeaweedFS},
		{"unknown", s3api.ProviderAws},
	}
	for _, tc := range cases {
		if got := parseProvider(tc.in); got != tc.want {
			t.Errorf("parseProvider(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestNewS3Client(t *testing.T) {
	t.Run("valid path-style", func(t *testing.T) {
		cfg := config.Static{
			HostBase:  "https://s3.example.com",
			AccessKey: "AK",
			SecretKey: "SK",
		}
		c, err := NewS3Client(context.Background(), cfg, config.Flags{})
		if err != nil {
			t.Fatal(err)
		}
		if c == nil {
			t.Fatal("nil client")
		}
	})

	t.Run("debug mode", func(t *testing.T) {
		cfg := config.Static{HostBase: "https://s3.example.com", AccessKey: "a", SecretKey: "s"}
		if c, err := NewS3Client(context.Background(), cfg, config.Flags{Debug: true}); err != nil || c == nil {
			t.Fatalf("client=%v err=%v", c, err)
		}
	})

	t.Run("bad header -> error", func(t *testing.T) {
		cfg := config.Static{HostBase: "https://s3.example.com", AccessKey: "a", SecretKey: "s"}
		if _, err := NewS3Client(context.Background(), cfg, config.Flags{Headers: []string{"no-separator"}}); err == nil {
			t.Error("expected error for bad header")
		}
	})

	t.Run("bad bucket lookup -> error", func(t *testing.T) {
		cfg := config.Static{HostBase: "https://s3.example.com", AccessKey: "a", SecretKey: "s", BucketLookup: "garbage"}
		if _, err := NewS3Client(context.Background(), cfg, config.Flags{}); err == nil {
			t.Error("expected error for bad bucket_lookup")
		}
	})

	t.Run("custom lookup", func(t *testing.T) {
		cfg := config.Static{
			HostBase:  "https://s3.example.com",
			AccessKey: "a", SecretKey: "s",
			BucketLookup: "https://%(bucket).s3.example.com",
		}
		if c, err := NewS3Client(context.Background(), cfg, config.Flags{}); err != nil || c == nil {
			t.Fatalf("client=%v err=%v", c, err)
		}
	})

	t.Run("missing endpoint -> error", func(t *testing.T) {
		_, err := NewS3Client(context.Background(), config.Static{AccessKey: "a", SecretKey: "s"}, config.Flags{})
		if err == nil {
			t.Error("expected error for missing endpoint")
		}
	})
}

func TestNewDumper(t *testing.T) {
	if d := NewDumper(http.DefaultTransport); d == nil {
		t.Fatal("nil")
	}
	if d := NewDumper(nil); d == nil {
		t.Fatal("nil base should fall back")
	}
}

func TestRedactedResponse(t *testing.T) {
	orig := &http.Response{Header: http.Header{}}
	orig.Header.Set("Set-Cookie", "secret=abc")
	orig.Header.Set("X-Amz-Security-Token", "token")
	orig.Header.Set("Content-Type", "text/plain")

	clone := redactedResponse(orig)
	if clone.Header.Get("Set-Cookie") != "REDACTED" {
		t.Error("Set-Cookie should be redacted")
	}
	if clone.Header.Get("X-Amz-Security-Token") != "REDACTED" {
		t.Error("token should be redacted")
	}
	if clone.Header.Get("Content-Type") != "text/plain" {
		t.Error("non-sensitive header should be untouched")
	}
	if orig.Header.Get("Set-Cookie") != "secret=abc" {
		t.Error("original mutated")
	}
}

func TestCustomBucketLookupNeedsRegion(t *testing.T) {
	withRegion := &CustomBucketLookup{
		Template:          "https://%(bucket).s3.%(region).example.com",
		BucketPlaceholder: "%(bucket)",
		RegionPlaceholder: "%(region)",
	}
	if !withRegion.NeedsRegion() {
		t.Error("should need region")
	}
	withoutRegion := &CustomBucketLookup{
		Template:          "https://%(bucket).s3.example.com",
		BucketPlaceholder: "%(bucket)",
	}
	if withoutRegion.NeedsRegion() {
		t.Error("should not need region")
	}
	missing := &CustomBucketLookup{
		Template:          "https://%(bucket).s3.example.com",
		BucketPlaceholder: "%(bucket)",
		RegionPlaceholder: "%(region)",
	}
	if missing.NeedsRegion() {
		t.Error("placeholder set but absent -> false")
	}
}

func TestResolveCustomEndpoint(t *testing.T) {
	c := &CustomBucketLookup{
		Template:          "https://%(bucket).s3.%(region).example.com",
		BucketPlaceholder: "%(bucket)",
		RegionPlaceholder: "%(region)",
	}
	u, err := c.ResolveCustomEndpoint("mybucket", "us-west-2")
	if err != nil {
		t.Fatal(err)
	}
	if u.String() != "https://mybucket.s3.us-west-2.example.com" {
		t.Errorf("got %s", u)
	}

	c2 := &CustomBucketLookup{
		Template:          "https://%(bucket).s3.example.com",
		BucketPlaceholder: "%(bucket)",
	}
	u2, _ := c2.ResolveCustomEndpoint("bk", "ignored")
	if u2.Host != "bk.s3.example.com" {
		t.Errorf("got %s", u2)
	}

	if _, err := (&CustomBucketLookup{}).ResolveCustomEndpoint("b", ""); err == nil {
		t.Error("expected error for empty template")
	}
	if _, err := c.ResolveCustomEndpoint("", ""); err == nil {
		t.Error("expected error for empty bucket")
	}
}

func TestParsePathAndNewClient(t *testing.T) {
	oldG, oldPath, oldCache := config.G, config.ConfPath, S3Clients
	defer func() { config.G, config.ConfPath, S3Clients = oldG, oldPath, oldCache }()
	config.G = &config.Config{}
	config.ConfPath = ""
	S3Clients = &kvcache.Cache[string, *s3api.Client]{}

	t.Run("unknown alias", func(t *testing.T) {
		config.G.S = nil
		_, _, err := ParsePathAndNewClient(context.Background(), "ghost:bucket")
		if err == nil {
			t.Error("expected error for unknown alias")
		}
	})

	t.Run("alias only with known alias", func(t *testing.T) {
		config.G.S = map[string]config.Static{
			"myalias": {HostBase: "https://s3.example.com", AccessKey: "a", SecretKey: "s"},
		}
		c, sp, err := ParsePathAndNewClient(context.Background(), "myalias")
		if c == nil {
			t.Error("expected non-nil client")
		}
		if sp == nil || sp.Alias != "myalias" {
			t.Errorf("bad path: %+v", sp)
		}
		if !errors.Is(err, s3path.ErrAliasOnly) {
			t.Errorf("expected ErrAliasOnly, got %v", err)
		}
	})

	t.Run("malformed arg", func(t *testing.T) {
		config.G.S = map[string]config.Static{}
		_, _, err := ParsePathAndNewClient(context.Background(), ":bucket")
		if err == nil {
			t.Error("expected error for malformed arg")
		}
	})

	t.Run("cached alias returns client", func(t *testing.T) {
		config.G.S = map[string]config.Static{
			"cached": {HostBase: "https://s3.example.com", AccessKey: "a", SecretKey: "s"},
		}
		c1, _, _ := ParsePathAndNewClient(context.Background(), "cached:bucket")
		if c1 == nil {
			t.Fatal("first call nil")
		}
		// 第二次应命中缓存 (即使清空 config 也仍返回)
		config.G.S = nil
		c2, _, err := ParsePathAndNewClient(context.Background(), "cached:bucket")
		if err != nil {
			t.Fatalf("second call err: %v", err)
		}
		if c2 == nil {
			t.Error("cached call nil")
		}
	})
}
