package client

import (
	"errors"
	"s3cli/internal/config"
	"s3cli/internal/s3path"
	"s3cli/pkg/kvcache"
	"s3cli/pkg/s3iface"
	"testing"
)

func TestNewClient(t *testing.T) {
	oldG, oldC, oldCache := config.G, config.G.C, S3Clients
	defer func() { config.G, config.G.C, S3Clients = oldG, oldC, oldCache }()
	config.G = &config.Config{}
	config.G.C = ""
	S3Clients = &kvcache.Cache[string, s3iface.S3Operations]{}

	valid := config.Static{HostBase: "https://s3.example.com", AccessKey: "a", SecretKey: "s"}

	t.Run("cache hit returns cached client", func(t *testing.T) {
		base, err := newS3Client(valid, config.Flags{})
		if err != nil {
			t.Fatal(err)
		}
		S3Clients.Set("preloaded", base)
		got, err := NewClient("preloaded", config.Static{})
		if err != nil {
			t.Fatal(err)
		}
		if got != s3iface.S3Operations(base) {
			t.Error("expected the preloaded client back")
		}
	})

	t.Run("cache miss constructs and stores", func(t *testing.T) {
		c, err := NewClient("fresh", valid)
		if err != nil {
			t.Fatal(err)
		}
		if c == nil {
			t.Fatal("nil client")
		}
		if _, ok := S3Clients.Get("fresh"); !ok {
			t.Error("client not stored in cache")
		}
	})

	t.Run("cache miss with invalid config", func(t *testing.T) {
		broken := config.Static{AccessKey: "a", SecretKey: "s"} // 缺少 HostBase
		if _, err := NewClient("broken", broken); err == nil {
			t.Error("expected error for invalid config")
		}
	})
}

func TestParsePathAndNewClient(t *testing.T) {
	oldG, oldC, oldCache := config.G, config.G.C, S3Clients
	defer func() { config.G, config.G.C, S3Clients = oldG, oldC, oldCache }()
	config.G = &config.Config{}
	config.G.C = ""
	S3Clients = &kvcache.Cache[string, s3iface.S3Operations]{}

	t.Run("unknown alias", func(t *testing.T) {
		config.G.S = nil
		_, p, err := ParsePathAndNewClient("ghost:bucket")
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
		c, sp, err := ParsePathAndNewClient("myalias")
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
		_, p, err := ParsePathAndNewClient(":bucket")
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
		c1, _, _ := ParsePathAndNewClient("cached:bucket")
		if c1 == nil {
			t.Fatal("first call nil")
		}
		// 第二次应命中缓存 (即使清空 config 也仍返回)
		config.G.S = nil
		c2, _, err := ParsePathAndNewClient("cached:bucket")
		if err != nil {
			t.Fatalf("second call err: %v", err)
		}
		if c2 == nil {
			t.Error("cached call nil")
		}
	})

	t.Run("broken alias config -> client error", func(t *testing.T) {
		config.G.S = map[string]config.Static{
			"broken": {AccessKey: "a", SecretKey: "s"}, // 缺少 HostBase
		}
		_, p, err := ParsePathAndNewClient("broken:bucket")
		if err == nil {
			t.Error("expected client error")
		}
		if p != nil {
			t.Errorf("expected nil Path on error, got %+v", p)
		}
	})
}
