package config

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveConfig(t *testing.T) {
	path := tempConfPath(t)
	want := map[string]Static{
		"full": {
			AccessKey: "ak", SecretKey: "sk", SessionToken: "tok",
			HostBase: "https://full.example.com", Region: "us-west-2",
			NoVerifySSL: true, BucketLookup: "https://%(bucket).full.example.com",
			DefaultMimeType: "application/octet-stream", MultipartChunkSizeMb: 64, MaxRetries: 5,
		},
		"minimal": {HostBase: "https://min.example.com", AccessKey: "ak2", SecretKey: "sk2"},
	}
	G.S = want
	if err := saveConfig(path); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config perm = %o, want 600", perm)
	}

	// 已存在目录不强制改权限；新建目录由 MkdirAll 设置为 0700
	nested := filepath.Join(t.TempDir(), "new", "dir", "conf")
	G.S = map[string]Static{"x": {HostBase: "https://x", AccessKey: "a", SecretKey: "s"}}
	if err := saveConfig(nested); err != nil {
		t.Fatal(err)
	}
	nestedDir, err := os.Stat(filepath.Dir(nested))
	if err != nil {
		t.Fatal(err)
	}
	if perm := nestedDir.Mode().Perm(); perm != 0o700 {
		t.Errorf("newly created config dir perm = %o, want 700", perm)
	}

	// 覆盖 G.S 后读回，字段应逐一致（裁剪不丢信息）。
	G.S = nil
	if err := readConfig(path); err != nil {
		t.Fatal(err)
	}
	for name, w := range want {
		if G.S[name] != w {
			t.Errorf("alias %s: got %+v, want %+v", name, G.S[name], w)
		}
	}
	if len(G.S) != len(want) {
		t.Errorf("got %d aliases, want %d", len(G.S), len(want))
	}
}

func TestSaveConfigErrors(t *testing.T) {
	snapshotHooks(t)
	path := filepath.Join(t.TempDir(), "conf")
	G.S = map[string]Static{"a": {HostBase: "https://a", AccessKey: "ak", SecretKey: "sk"}}

	injects := map[string]func(){
		"mkdirAll":   func() { mkdirAll = func(string, os.FileMode) error { return errors.New("mkdir") } },
		"createTemp": func() { createTemp = func(string, string) (*os.File, error) { return nil, errors.New("tmp") } },
		"chmodFile":  func() { chmodFile = func(*os.File, os.FileMode) error { return errors.New("chmod") } },
		"encodeTOML": func() { encodeTOML = func(io.Writer, any) error { return errors.New("encode") } },
		"syncFile":   func() { syncFile = func(*os.File) error { return errors.New("sync") } },
		"closeFile":  func() { closeFile = func(*os.File) error { return errors.New("close") } },
		"rename":     func() { rename = func(string, string) error { return errors.New("rename") } },
	}
	for name, inject := range injects {
		t.Run(name, func(t *testing.T) {
			snapshotHooks(t)
			inject()
			if err := saveConfig(path); err == nil {
				t.Error("want error")
			}
			// 不应残留临时文件
			entries, _ := os.ReadDir(filepath.Dir(path))
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), ".s3cli-") {
					t.Errorf("temp file leaked: %s", e.Name())
				}
			}
		})
	}
}

func TestBuildOutputMap(t *testing.T) {
	// 全部默认/空值 → 只保留必写三项
	m := buildOutputMap(Static{AccessKey: "ak", SecretKey: "sk", HostBase: "https://h"})
	if len(m) != 3 {
		t.Errorf("default map should have 3 keys, got %v", m)
	}
	for _, k := range []string{"access_key", "secret_key", "host_base"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing required key %q: %v", k, m)
		}
	}

	// multipart_chunk_size_mb 为 0 或默认 15 时裁剪
	for _, v := range []int{0, DefaultPartSizeMB} {
		m = buildOutputMap(Static{AccessKey: "a", SecretKey: "s", HostBase: "h", MultipartChunkSizeMb: v})
		if _, ok := m["multipart_chunk_size_mb"]; ok {
			t.Errorf("multipart_chunk_size_mb=%d should be pruned: %v", v, m)
		}
	}

	// 全部非默认 → 保留所有可选键
	m = buildOutputMap(Static{
		AccessKey: "ak", SecretKey: "sk", HostBase: "https://h",
		SessionToken: "t", Region: "r", DefaultMimeType: "x", NoVerifySSL: true,
		BucketLookup: "dns", MultipartChunkSizeMb: 64, MaxRetries: 3,
	})
	for _, k := range []string{"access_key", "secret_key", "host_base", "session_token", "region",
		"default_mime_type", "no_verify_ssl", "bucket_lookup", "multipart_chunk_size_mb", "max_retries"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing key %q: %v", k, m)
		}
	}

	// tls_min_version: 空/默认裁剪, 非默认保留
	for _, v := range []string{"", DefaultTLSMinVersion} {
		m = buildOutputMap(Static{AccessKey: "a", SecretKey: "s", HostBase: "h", TLSMinVersion: v})
		if _, ok := m["tls_min_version"]; ok {
			t.Errorf("tls_min_version=%q should be pruned: %v", v, m)
		}
	}
	m = buildOutputMap(Static{AccessKey: "a", SecretKey: "s", HostBase: "h", TLSMinVersion: "1.0"})
	if m["tls_min_version"] != "1.0" {
		t.Errorf("tls_min_version = %v, want 1.0", m["tls_min_version"])
	}
}

// TestSaveConfigSyncsDir rename 之后应 best-effort fsync 目录, 保证 rename 落盘。
func TestSaveConfigSyncsDir(t *testing.T) {
	snapshotHooks(t)
	path := filepath.Join(t.TempDir(), "conf")
	G.S = map[string]Static{"a": {HostBase: "https://a", AccessKey: "ak", SecretKey: "sk"}}

	called := 0
	syncDirFn = func(dir string) error {
		called++
		if dir != filepath.Dir(path) {
			t.Errorf("syncDirFn dir = %q, want %q", dir, filepath.Dir(path))
		}
		return nil
	}
	if err := saveConfig(path); err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Errorf("syncDirFn called %d times, want 1", called)
	}
}
