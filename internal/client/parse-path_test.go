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
	S3Clients = &kvcache.Cache[string, cachedBackend]{}

	valid := config.Static{HostBase: "https://s3.example.com", AccessKey: "a", SecretKey: "s"}

	t.Run("cache hit with same static returns cached client", func(t *testing.T) {
		base, err := newS3Client(valid, config.Flags{})
		if err != nil {
			t.Fatal(err)
		}
		S3Clients.Set("preloaded", cachedBackend{client: base, static: valid})
		got, err := NewClient("preloaded", valid)
		if err != nil {
			t.Fatal(err)
		}
		if got != s3iface.S3Operations(base) {
			t.Error("expected the preloaded client back")
		}
	})

	t.Run("cache hit with changed static rebuilds client", func(t *testing.T) {
		base, err := newS3Client(valid, config.Flags{})
		if err != nil {
			t.Fatal(err)
		}
		S3Clients.Set("stale", cachedBackend{client: base, static: valid})
		changed := valid
		changed.AccessKey = "new-ak"
		got, err := NewClient("stale", changed)
		if err != nil {
			t.Fatal(err)
		}
		if got == s3iface.S3Operations(base) {
			t.Error("expected a rebuilt client after static change")
		}
		cached, ok := S3Clients.Get("stale")
		if !ok || cached.static != changed {
			t.Error("cache should store the updated static")
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
	S3Clients = &kvcache.Cache[string, cachedBackend]{}

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

	t.Run("cached alias returns same client", func(t *testing.T) {
		static := config.Static{HostBase: "https://s3.example.com", AccessKey: "a", SecretKey: "s"}
		config.G.S = map[string]config.Static{"cached": static}
		c1, _, err := ParsePathAndNewClient("cached:bucket")
		if err != nil || c1 == nil {
			t.Fatalf("first call err=%v client=%v", err, c1)
		}
		// 第二次应命中缓存并返回同一实例
		c2, _, err := ParsePathAndNewClient("cached:bucket")
		if err != nil {
			t.Fatalf("second call err: %v", err)
		}
		if c1 != c2 {
			t.Error("expected cached client instance")
		}
	})

	t.Run("alias config change rebuilds client", func(t *testing.T) {
		static := config.Static{HostBase: "https://s3.example.com", AccessKey: "a", SecretKey: "s"}
		config.G.S = map[string]config.Static{"mutable": static}
		c1, _, err := ParsePathAndNewClient("mutable:bucket")
		if err != nil || c1 == nil {
			t.Fatalf("first call err=%v client=%v", err, c1)
		}
		changed := static
		changed.AccessKey = "rotated"
		config.G.S = map[string]config.Static{"mutable": changed}
		c2, _, err := ParsePathAndNewClient("mutable:bucket")
		if err != nil {
			t.Fatalf("second call err: %v", err)
		}
		if c1 == c2 {
			t.Error("expected a rebuilt client after config change")
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
