package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/ini.v1"
)

// snapshotGlobals 保存 G 与 ConfPath, 返回 restore; 测试必须用它隔离全局状态。
func snapshotGlobals(t *testing.T) func() {
	t.Helper()
	oldG, oldPath := G, ConfPath
	G = &Config{}
	ConfPath = ""
	return func() {
		G, ConfPath = oldG, oldPath
	}
}

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, ".s3cli")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestResolveBucketLookup(t *testing.T) {
	cases := []struct {
		in       string
		wantMode string
		wantTpl  string
		wantErr  bool
	}{
		{"", BucketLookupPath, "", false},
		{"path", BucketLookupPath, "", false},
		{"PATH", BucketLookupPath, "", false}, // 大小写不敏感
		{"dns", BucketLookupDNS, "", false},
		{"DNS", BucketLookupDNS, "", false},
		{"https://%(bucket).s3.example.com", BucketLookupCustom, "https://%(bucket).s3.example.com", false},
		{"https://%(bucket).s3.%(region).amazonaws.com", BucketLookupCustom, "https://%(bucket).s3.%(region).amazonaws.com", false},
		{"garbage", "", "", true},
		{"https://example.com", "", "", true}, // 缺 %(bucket)
	}
	for _, tc := range cases {
		s := &Static{BucketLookup: tc.in}
		mode, tpl, err := s.ResolveBucketLookup()
		if tc.wantErr {
			if err == nil {
				t.Errorf("BucketLookup=%q: expected error, got nil", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("BucketLookup=%q: unexpected error %v", tc.in, err)
			continue
		}
		if mode != tc.wantMode || tpl != tc.wantTpl {
			t.Errorf("BucketLookup=%q: got (%q,%q), want (%q,%q)", tc.in, mode, tpl, tc.wantMode, tc.wantTpl)
		}
	}
}

func TestValidateCustomTemplate(t *testing.T) {
	good := []string{
		"https://%(bucket).s3.example.com",
		"https://%(bucket).s3.%(region).amazonaws.com",
		"http://%(bucket).local:9000",
	}
	bad := []string{
		"",                                 // 空
		"https://example.com",              // 无 %(bucket)
		"https://%(bucket)",                // %(bucket) 在末尾
		"https://%(bucket)%(bucket).x.com", // 多个 %(bucket)
		"https://%(bucket).s3.%(region).%(region).com", // 多个 %(region)
		"%(bucket)", // 解析后无 host
	}
	for _, tpl := range good {
		if !validateCustomTemplate(tpl) {
			t.Errorf("expected %q to be valid", tpl)
		}
	}
	for _, tpl := range bad {
		if validateCustomTemplate(tpl) {
			t.Errorf("expected %q to be invalid", tpl)
		}
	}
}

func TestEnsureConfPath(t *testing.T) {
	restore := snapshotGlobals(t)
	defer restore()

	// 空时回退到 $HOME/.s3cli
	ConfPath = ""
	got := ensureConfPath()
	want := filepath.Join(os.Getenv("HOME"), ".s3cli")
	if got != want {
		t.Errorf("ensureConfPath() = %q, want %q", got, want)
	}
	if ConfPath != want {
		t.Errorf("ConfPath not set, got %q", ConfPath)
	}
	// 已设置时直接返回
	ConfPath = "/tmp/custom"
	if g := ensureConfPath(); g != "/tmp/custom" {
		t.Errorf("ensureConfPath() = %q, want /tmp/custom", g)
	}
}

func TestMaskSecret(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "****"},
		{"abc", "****"},
		{"abcd", "****"},
		{"abcde", "****bcde"},
		{"0123456789", "****6789"},
	}
	for _, tc := range cases {
		if got := maskSecret(tc.in); got != tc.want {
			t.Errorf("maskSecret(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestStringInSlice(t *testing.T) {
	if !stringInSlice("b", []string{"a", "b", "c"}) {
		t.Error("expected true for present")
	}
	if stringInSlice("x", []string{"a", "b", "c"}) {
		t.Error("expected false for absent")
	}
	if stringInSlice("a", nil) {
		t.Error("expected false for nil list")
	}
}

func TestSaveConfig(t *testing.T) {
	restore := snapshotGlobals(t)
	defer restore()

	cfg := ini.Empty()
	sec, _ := cfg.NewSection("myalias")
	_, _ = sec.NewKey("access_key", "AKIA123")
	_, _ = sec.NewKey("host_base", "https://s3.example.com")

	dir := t.TempDir()
	p := filepath.Join(dir, "sub", "nested", ".s3cli") // 测 MkdirAll
	if err := saveConfig(cfg, p); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}

	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("perm = %o, want 0600", mode)
	}
	// 内容可重新加载
	if _, err := ini.Load(p); err != nil {
		t.Fatalf("reload: %v", err)
	}
}

func TestLoadConf(t *testing.T) {
	restore := snapshotGlobals(t)
	defer restore()

	t.Run("valid", func(t *testing.T) {
		content := "[alias1]\naccess_key = AK\nsecret_key = SK\nhost_base = https://s3.example.com\nverify_ssl = true\n" +
			"[alias2]\naccess_key = AK2\nsecret_key = SK2\nhost_base = https://s3b.example.com\n"
		ConfPath = writeTempConfig(t, content)
		if err := LoadConf(); err != nil {
			t.Fatal(err)
		}
		if len(G.S) != 2 {
			t.Fatalf("expected 2 sections, got %d", len(G.S))
		}
		a1 := G.S["alias1"]
		if a1.AccessKey != "AK" || a1.HostBase != "https://s3.example.com" || !a1.VerifySSL {
			t.Errorf("alias1 misparsed: %#v", a1)
		}
		a2 := G.S["alias2"]
		// verify_ssl 缺失时应默认 true
		if !a2.VerifySSL {
			t.Error("expected VerifySSL default true when key absent")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		ConfPath = filepath.Join(t.TempDir(), "nope")
		if err := LoadConf(); err == nil {
			t.Error("expected error for missing file")
		}
	})

	t.Run("empty file", func(t *testing.T) {
		ConfPath = writeTempConfig(t, "")
		if err := LoadConf(); err == nil {
			t.Error("expected error for empty file")
		}
	})

	t.Run("malformed", func(t *testing.T) {
		ConfPath = writeTempConfig(t, "[bad\nkey = =\n")
		if err := LoadConf(); err == nil {
			t.Error("expected error for malformed ini")
		}
	})
}

func TestListAliasConf(t *testing.T) {
	restore := snapshotGlobals(t)
	defer restore()

	t.Run("missing file returns error", func(t *testing.T) {
		ConfPath = filepath.Join(t.TempDir(), "nope")
		if err := ListAliasConf(nil); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("empty file returns error", func(t *testing.T) {
		ConfPath = writeTempConfig(t, "")
		if err := ListAliasConf(nil); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("valid sections no error", func(t *testing.T) {
		content := "[prod]\nhost_base = https://s3.example.com\naccess_key = AK\nsecret_key = SECRET123456\n"
		ConfPath = writeTempConfig(t, content)
		if err := ListAliasConf(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestConfirmDelete(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"y\n", true},
		{"Y\n", true},
		{"yes\n", true},
		{"  YES  \n", true},
		{"n\n", false},
		{"no\n", false},
		{"\n", false},
		{"", false}, // EOF
	}
	for _, tc := range cases {
		r := bufio.NewReader(strings.NewReader(tc.input))
		if got := confirmDelete(r, "x"); got != tc.want {
			t.Errorf("input %q: got %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestDelConf(t *testing.T) {
	restore := snapshotGlobals(t)
	defer restore()

	t.Run("empty name", func(t *testing.T) {
		ConfPath = writeTempConfig(t, "[a]\nhost_base = x\n")
		if err := delConf("  "); err == nil {
			t.Error("expected error for empty name")
		}
	})

	t.Run("reject DEFAULT", func(t *testing.T) {
		ConfPath = writeTempConfig(t, "[a]\nhost_base = x\n")
		if err := delConf("DEFAULT"); err == nil {
			t.Error("expected error for DEFAULT")
		}
		if err := delConf("default"); err == nil {
			t.Error("expected error for default (case-insensitive)")
		}
	})

	t.Run("unknown section", func(t *testing.T) {
		ConfPath = writeTempConfig(t, "[a]\nhost_base = x\n")
		if err := delConf("missing"); err == nil {
			t.Error("expected error for missing section")
		}
	})

	t.Run("delete existing", func(t *testing.T) {
		ConfPath = writeTempConfig(t, "[a]\nhost_base = x\n[b]\nhost_base = y\n")
		if err := delConf("a"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		cfg, _ := ini.Load(ConfPath)
		if cfg.HasSection("a") {
			t.Error("section a should be deleted")
		}
		if !cfg.HasSection("b") {
			t.Error("section b should remain")
		}
	})
}

func TestDelConfTopLevel(t *testing.T) {
	restore := snapshotGlobals(t)
	defer restore()

	ConfPath = writeTempConfig(t, "[a]\nhost_base = x\n[b]\nhost_base = y\n")
	// 非 TTY (go test 的 stdin 是管道) → 直接删除无需确认
	if err := DelConf([]string{"a"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cfg, _ := ini.Load(ConfPath)
	if cfg.HasSection("a") {
		t.Error("section a should be deleted")
	}

	// 缺失配置文件
	ConfPath = filepath.Join(t.TempDir(), "nope")
	if err := DelConf([]string{"a"}); err == nil {
		t.Error("expected error for missing file")
	}
}
