package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/ini.v1"
)

func TestSaveConfigUsesOwnerOnlyPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config")
	cfg := ini.Empty()
	sec, err := cfg.NewSection("prod")
	if err != nil {
		t.Fatal(err)
	}
	sec.Key("secret_key").SetValue("secret")
	if err := saveConfig(cfg, path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("config permissions = %o, want 600", got)
	}
}

func TestLoadConfDefaultsToVerifySSL(t *testing.T) {
	oldPath, oldSections := ConfPath, G.S
	t.Cleanup(func() {
		ConfPath = oldPath
		SetSections(oldSections)
	})
	ConfPath = filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(ConfPath, []byte("[prod]\nhost_base=https://s3.example.test\naccess_key=key\nsecret_key=secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := LoadConf(); err != nil {
		t.Fatal(err)
	}
	if !G.S["prod"].VerifySSL {
		t.Fatal("verify_ssl should default to true when omitted")
	}
}
