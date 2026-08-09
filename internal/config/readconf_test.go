package config

import (
	"errors"
	"os"
	"testing"
)

func TestLoadConf(t *testing.T) {
	path := tempConfPath(t)

	// 文件不存在 → ErrConfigNotFoundOrEmpty
	if err := LoadConf(); !errors.Is(err, ErrConfigNotFoundOrEmpty) {
		t.Errorf("want ErrConfigNotFoundOrEmpty, got %v", err)
	}

	// 写入配置后 LoadConf 应解析进 G.S
	G.S = map[string]Static{"a": {HostBase: "https://a", AccessKey: "ak", SecretKey: "sk"}}
	if err := saveConfig(path); err != nil {
		t.Fatal(err)
	}
	G.S = nil
	if err := LoadConf(); err != nil {
		t.Fatal(err)
	}
	if G.S["a"].HostBase != "https://a" {
		t.Errorf("config not loaded: %+v", G.S)
	}
}

func TestReadConfig(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		path := tempConfPath(t)
		if err := readConfig(path); !errors.Is(err, ErrConfigNotFoundOrEmpty) {
			t.Errorf("want ErrConfigNotFoundOrEmpty, got %v", err)
		}
	})

	t.Run("empty file", func(t *testing.T) {
		path := tempConfPath(t)
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := readConfig(path); !errors.Is(err, ErrConfigNotFoundOrEmpty) {
			t.Errorf("want ErrConfigNotFoundOrEmpty, got %v", err)
		}
	})

	t.Run("valid toml", func(t *testing.T) {
		path := tempConfPath(t)
		content := "[a]\n  host_base = \"https://a.example.com\"\n  access_key = \"ak\"\n  secret_key = \"sk\"\n"
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		G.S = nil
		if err := readConfig(path); err != nil {
			t.Fatal(err)
		}
		if got := G.S["a"]; got.HostBase != "https://a.example.com" || got.AccessKey != "ak" {
			t.Errorf("unexpected alias: %+v", got)
		}
	})

	t.Run("invalid toml", func(t *testing.T) {
		path := tempConfPath(t)
		if err := os.WriteFile(path, []byte("[broken"), 0o600); err != nil {
			t.Fatal(err)
		}
		err := readConfig(path)
		if err == nil || errors.Is(err, ErrConfigNotFoundOrEmpty) {
			t.Errorf("want toml decode error, got %v", err)
		}
	})

	t.Run("stat other error", func(t *testing.T) {
		snapshotHooks(t)
		path := tempConfPath(t)
		osStat = func(string) (os.FileInfo, error) { return nil, errors.New("boom") }
		if err := readConfig(path); err == nil || err.Error() != "boom" {
			t.Errorf("want wrapped stat error, got %v", err)
		}
	})

	t.Run("stat not exist via hook", func(t *testing.T) {
		snapshotHooks(t)
		path := tempConfPath(t)
		osStat = func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
		if err := readConfig(path); !errors.Is(err, ErrConfigNotFoundOrEmpty) {
			t.Errorf("want ErrConfigNotFoundOrEmpty, got %v", err)
		}
	})
}
