package client

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"s3cli/internal/config"
	"s3cli/internal/s3path"
	"s3cli/pkg/kvcache"
	"s3cli/pkg/s3iface"
)

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
	S3Clients = &kvcache.Cache[string, s3iface.S3Operations]{}

	t.Run("unknown alias", func(t *testing.T) {
		config.G.S = nil
		_, p, err := ParsePathAndNewClient(context.Background(), "ghost:bucket")
		if err == nil {
			t.Error("expected error for unknown alias")
		}
		if p != nil {
			t.Errorf("expected nil Path on error, got %+v", p)
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
		_, p, err := ParsePathAndNewClient(context.Background(), ":bucket")
		if err == nil {
			t.Error("expected error for malformed arg")
		}
		if p != nil {
			t.Errorf("expected nil Path on error, got %+v", p)
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
