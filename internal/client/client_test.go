package client

import (
	"crypto/tls"
	"testing"

	"s3cli/internal/config"
)

func TestNewS3Client(t *testing.T) {
	t.Run("valid path-style", func(t *testing.T) {
		cfg := config.Static{
			HostBase:  "https://s3.example.com",
			AccessKey: "AK",
			SecretKey: "SK",
		}
		c, err := newS3Client(cfg, config.Flags{})
		if err != nil {
			t.Fatal(err)
		}
		if c == nil {
			t.Fatal("nil client")
		}
	})

	t.Run("debug mode", func(t *testing.T) {
		cfg := config.Static{HostBase: "https://s3.example.com", AccessKey: "a", SecretKey: "s"}
		if c, err := newS3Client(cfg, config.Flags{Debug: true}); err != nil || c == nil {
			t.Fatalf("client=%v err=%v", c, err)
		}
	})

	t.Run("user agent override + suffix", func(t *testing.T) {
		cfg := config.Static{HostBase: "https://s3.example.com", AccessKey: "a", SecretKey: "s"}
		flags := config.Flags{UserAgent: "s3cli-custom", UserAgentSuffix: "-dev"}
		if c, err := newS3Client(cfg, flags); err != nil || c == nil {
			t.Fatalf("client=%v err=%v", c, err)
		}
	})

	t.Run("dns bucket lookup", func(t *testing.T) {
		cfg := config.Static{
			HostBase:     "https://s3.example.com",
			AccessKey:    "a",
			SecretKey:    "s",
			BucketLookup: "dns",
		}
		if c, err := newS3Client(cfg, config.Flags{}); err != nil || c == nil {
			t.Fatalf("client=%v err=%v", c, err)
		}
	})

	t.Run("bad header -> error", func(t *testing.T) {
		cfg := config.Static{HostBase: "https://s3.example.com", AccessKey: "a", SecretKey: "s"}
		if _, err := newS3Client(cfg, config.Flags{Headers: []string{"no-separator"}}); err == nil {
			t.Error("expected error for bad header")
		}
	})

	t.Run("bad bucket lookup -> error", func(t *testing.T) {
		cfg := config.Static{HostBase: "https://s3.example.com", AccessKey: "a", SecretKey: "s", BucketLookup: "garbage"}
		if _, err := newS3Client(cfg, config.Flags{}); err == nil {
			t.Error("expected error for bad bucket_lookup")
		}
	})

	t.Run("custom lookup", func(t *testing.T) {
		cfg := config.Static{
			HostBase:  "https://s3.example.com",
			AccessKey: "a", SecretKey: "s",
			BucketLookup: "https://%(bucket).s3.example.com",
		}
		if c, err := newS3Client(cfg, config.Flags{}); err != nil || c == nil {
			t.Fatalf("client=%v err=%v", c, err)
		}
	})

	t.Run("missing endpoint -> error", func(t *testing.T) {
		_, err := newS3Client(config.Static{AccessKey: "a", SecretKey: "s"}, config.Flags{})
		if err == nil {
			t.Error("expected error for missing endpoint")
		}
	})
}

// TestApplyGlobalOverrides 验证全局 flag 对别名静态配置的覆盖规则。
func TestApplyGlobalOverrides(t *testing.T) {
	base := config.Static{HostBase: "https://a.example.com", NoVerifySSL: false}

	// 无全局 flag → 原样
	if got := applyGlobalOverrides(base, config.Flags{}); got != base {
		t.Errorf("no flags should not change cfg: %+v", got)
	}

	// --host-base 覆盖
	got := applyGlobalOverrides(base, config.Flags{HostBase: "https://b.example.com"})
	if got.HostBase != "https://b.example.com" {
		t.Errorf("host-base override failed: %+v", got)
	}
	if got.NoVerifySSL {
		t.Error("no-verify-ssl should stay false")
	}

	// --no-verify-ssl 与别名配置取或
	if got := applyGlobalOverrides(base, config.Flags{NoVerifySSL: true}); !got.NoVerifySSL {
		t.Error("no-verify-ssl flag should force true")
	}
	if got := applyGlobalOverrides(config.Static{HostBase: "x", NoVerifySSL: true}, config.Flags{}); !got.NoVerifySSL {
		t.Error("alias no_verify_ssl should be preserved without flag")
	}

	// --host-base 覆盖自定义模板寻址时, 模板降级为 path
	tmpl := config.Static{
		HostBase:  "https://a.example.com",
		AccessKey: "a", SecretKey: "s",
		BucketLookup: "https://%(bucket).a.example.com",
	}
	got = applyGlobalOverrides(tmpl, config.Flags{HostBase: "https://b.example.com"})
	if got.HostBase != "https://b.example.com" || got.BucketLookup != "" {
		t.Errorf("custom template should degrade to path when host-base set: %+v", got)
	}
	// 无 --host-base 时模板保留
	if got := applyGlobalOverrides(tmpl, config.Flags{}); got.BucketLookup == "" {
		t.Error("custom template should be preserved without host-base flag")
	}
}

func TestTLSMinVersion(t *testing.T) {
	cases := []struct {
		in      string
		want    uint16
		wantErr bool
	}{
		{"", tls.VersionTLS12, false},
		{"1.2", tls.VersionTLS12, false},
		{"TLS1.2", tls.VersionTLS12, false},
		{"1.0", tls.VersionTLS10, false},
		{"1.1", tls.VersionTLS11, false},
		{"1.3", tls.VersionTLS13, false},
		{" 1.3 ", tls.VersionTLS13, false},
		{"2.0", 0, true},
		{"tls", 0, true},
	}
	for _, tc := range cases {
		got, err := tlsMinVersion(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("tlsMinVersion(%q) should error", tc.in)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("tlsMinVersion(%q) = (%v, %v), want (%d, nil)", tc.in, got, err, tc.want)
		}
	}
}

// TestNewS3ClientTLSMinVersion 别名 tls_min_version 应透传到 Transport 的 TLS 配置。
func TestNewS3ClientTLSMinVersion(t *testing.T) {
	cfg := config.Static{HostBase: "https://s3.example.com", AccessKey: "a", SecretKey: "s", TLSMinVersion: "1.0"}
	c, err := newS3Client(cfg, config.Flags{})
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		t.Fatal("nil client")
	}

	cfg.TLSMinVersion = "9.9"
	if _, err := newS3Client(cfg, config.Flags{}); err == nil {
		t.Error("expected error for invalid tls_min_version")
	}
}
