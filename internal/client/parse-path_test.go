package client

import (
	"errors"
	"testing"

	"github.com/onmk613/s3cli/internal/config"
	"github.com/onmk613/s3cli/internal/s3path"
)

func TestResolverClient(t *testing.T) {
	oldG, oldC := config.G, config.G.C
	defer func() { config.G, config.G.C = oldG, oldC }()
	config.G = &config.Config{}
	config.G.C = ""

	valid := config.Static{HostBase: "https://s3.example.com", AccessKey: "a", SecretKey: "s"}

	t.Run("same static returns cached client", func(t *testing.T) {
		r := NewResolver()
		first, err := r.Client("preloaded", valid)
		if err != nil {
			t.Fatal(err)
		}
		got, err := r.Client("preloaded", valid)
		if err != nil {
			t.Fatal(err)
		}
		if got != first {
			t.Error("expected the same client instance from the resolver cache")
		}
	})

	t.Run("changed static rebuilds client", func(t *testing.T) {
		r := NewResolver()
		first, err := r.Client("stale", valid)
		if err != nil {
			t.Fatal(err)
		}
		changed := valid
		changed.AccessKey = "new-ak"
		got, err := r.Client("stale", changed)
		if err != nil {
			t.Fatal(err)
		}
		if got == first {
			t.Error("expected a rebuilt client after static change")
		}
		cached, ok := r.cache.Get("stale")
		if !ok || cached.static != changed {
			t.Error("cache should store the updated static")
		}
	})

	t.Run("cache is scoped to one resolver", func(t *testing.T) {
		// 两条命令各自创建 Resolver, 不能共享客户端 (旧实现是包级全局缓存,
		// 命令之间会互相污染, 也让 flags/配置变化难以失效)。
		first, err := NewResolver().Client("scoped", valid)
		if err != nil {
			t.Fatal(err)
		}
		second, err := NewResolver().Client("scoped", valid)
		if err != nil {
			t.Fatal(err)
		}
		if first == second {
			t.Error("resolvers must not share cached clients")
		}
	})

	t.Run("invalid config", func(t *testing.T) {
		broken := config.Static{AccessKey: "a", SecretKey: "s"} // 缺少 HostBase
		if _, err := NewResolver().Client("broken", broken); err == nil {
			t.Error("expected error for invalid config")
		}
	})

	t.Run("New does not cache", func(t *testing.T) {
		first, err := New(valid)
		if err != nil {
			t.Fatal(err)
		}
		second, err := New(valid)
		if err != nil {
			t.Fatal(err)
		}
		if first == second {
			t.Error("New must construct a fresh client each call")
		}
	})
}

func TestResolverParsePath(t *testing.T) {
	oldG, oldC := config.G, config.G.C
	defer func() { config.G, config.G.C = oldG, oldC }()
	config.G = &config.Config{}
	config.G.C = ""

	t.Run("unknown alias", func(t *testing.T) {
		config.G.S = nil
		_, p, err := NewResolver().ParsePath("ghost:bucket")
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
		c, sp, err := NewResolver().ParsePath("myalias")
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
		_, p, err := NewResolver().ParsePath(":bucket")
		if err == nil {
			t.Error("expected error for malformed arg")
		}
		if p != nil {
			t.Errorf("expected nil Path on error, got %+v", p)
		}
	})

	t.Run("same resolver reuses client", func(t *testing.T) {
		static := config.Static{HostBase: "https://s3.example.com", AccessKey: "a", SecretKey: "s"}
		config.G.S = map[string]config.Static{"cached": static}
		r := NewResolver()
		c1, _, err := r.ParsePath("cached:bucket")
		if err != nil || c1 == nil {
			t.Fatalf("first call err=%v client=%v", err, c1)
		}
		// 第二次应命中本次命令的缓存并返回同一实例
		c2, _, err := r.ParsePath("cached:bucket")
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
		r := NewResolver()
		c1, _, err := r.ParsePath("mutable:bucket")
		if err != nil || c1 == nil {
			t.Fatalf("first call err=%v client=%v", err, c1)
		}
		changed := static
		changed.AccessKey = "rotated"
		config.G.S = map[string]config.Static{"mutable": changed}
		c2, _, err := r.ParsePath("mutable:bucket")
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
		_, p, err := NewResolver().ParsePath("broken:bucket")
		if err == nil {
			t.Error("expected client error")
		}
		if p != nil {
			t.Errorf("expected nil Path on error, got %+v", p)
		}
	})
}
