package config

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// snapshotHooks 保存全部可注入钩子并在测试结束时恢复。
func snapshotHooks(t *testing.T) {
	t.Helper()
	old := struct {
		isTerminal   func(int) bool
		readPassword func(int) ([]byte, error)
		userHomeDir  func() (string, error)
		osStat       func(string) (os.FileInfo, error)
		mkdirAll     func(string, os.FileMode) error
		createTemp   func(string, string) (*os.File, error)
		chmodFile    func(*os.File, os.FileMode) error
		encodeTOML   func(io.Writer, any) error
		syncFile     func(*os.File) error
		closeFile    func(*os.File) error
		rename       func(string, string) error
		syncDirFn    func(string) error
	}{
		isTerminal, readPassword, userHomeDir, osStat,
		mkdirAll, createTemp, chmodFile, encodeTOML,
		syncFile, closeFile, rename, syncDirFn,
	}
	t.Cleanup(func() {
		isTerminal, readPassword, userHomeDir, osStat = old.isTerminal, old.readPassword, old.userHomeDir, old.osStat
		mkdirAll, createTemp, chmodFile, encodeTOML = old.mkdirAll, old.createTemp, old.chmodFile, old.encodeTOML
		syncFile, closeFile, rename, syncDirFn = old.syncFile, old.closeFile, old.rename, old.syncDirFn
	})
}

// snapshotGlobal 保存 G 全局状态并在测试结束时恢复。
func snapshotGlobal(t *testing.T) {
	t.Helper()
	oldC, oldF, oldS := G.C, G.F, G.S
	t.Cleanup(func() { G.C, G.F, G.S = oldC, oldF, oldS })
}

// tempConfPath 把 G.C 指向临时配置文件路径并清空 G.S。
func tempConfPath(t *testing.T) string {
	t.Helper()
	snapshotGlobal(t)
	path := filepath.Join(t.TempDir(), "conf")
	G.C = path
	G.S = nil
	return path
}

// feedStdin 把 os.Stdin 替换为管道，写入 input 后关闭。
func feedStdin(t *testing.T, input string) {
	t.Helper()
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = pr
	t.Cleanup(func() { os.Stdin = old })
	go func() {
		_, _ = pw.WriteString(input)
		_ = pw.Close()
	}()
}

func TestDefaultConfigPath(t *testing.T) {
	snapshotHooks(t)

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	userHomeDir = func() (string, error) { return home, nil }
	if got := DefaultConfigPath(); got != filepath.Join(home, ".s3cli") {
		t.Errorf("DefaultConfigPath = %q, want %q", got, filepath.Join(home, ".s3cli"))
	}

	userHomeDir = func() (string, error) { return "", errors.New("no home") }
	if got := DefaultConfigPath(); got != "" {
		t.Errorf("DefaultConfigPath = %q, want empty on error", got)
	}
}

func TestResolveBucketLookup(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		mode    string
		tpl     string
		wantErr bool
	}{
		{"empty defaults to path", "", BucketLookupPath, "", false},
		{"path", "path", BucketLookupPath, "", false},
		{"path uppercase", "PATH", BucketLookupPath, "", false},
		{"dns", "dns", BucketLookupDNS, "", false},
		{"custom", "https://%(bucket).s3.example.com", BucketLookupCustom, "https://%(bucket).s3.example.com", false},
		{"custom with region", "https://%(bucket).s3.%(region).example.com", BucketLookupCustom, "https://%(bucket).s3.%(region).example.com", false},
		{"invalid", "garbage", "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := Static{BucketLookup: tc.in}
			mode, tpl, err := s.ResolveBucketLookup()
			if tc.wantErr {
				if err == nil {
					t.Fatal("want error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode != tc.mode || tpl != tc.tpl {
				t.Errorf("got (%q, %q), want (%q, %q)", mode, tpl, tc.mode, tc.tpl)
			}
		})
	}
}

func TestValidateCustomTemplate(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"https://%(bucket).s3.example.com", true},
		{"https://%(bucket).s3.%(region).example.com", true},
		{"https://example.com", false},                           // 缺 %(bucket)
		{"https://example.com/%(bucket)", false},                 // %(bucket) 在最末尾
		{"https://%(bucket)x.example.com/%(bucket)/y", false},    // %(bucket) 出现两次
		{"https://%(bucket).x.%(region).y.%(region).com", false}, // %(region) 出现两次
		{"https://%(bucket)..example.com", false},                // host 连续点
		{"https:///%(bucket)/x", false},                          // host 为空
		{"https://%(bucket).example.com/%zz", false},             // 替换后非法 URL 转义
		{"://%(bucket).x.com", false},                            // 非法 URL
		{"", false},                                              // 空
	}
	for _, tc := range cases {
		if got := validateCustomTemplate(tc.in); got != tc.want {
			t.Errorf("validateCustomTemplate(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
