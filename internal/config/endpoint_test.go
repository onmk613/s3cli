package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestValidateEndpointRejectsMissingScheme 是安全回归测试。
//
// 修复前 host_base 只被检查"是否为空", 而 api.New 在缺少 scheme 时会补
// "http://" —— 于是 `alias add x s3.example.com AK SK` (漏写 https://) 会把
// 凭证与对象数据静默走明文 HTTP 发出。这是最常见的输入失误, 必须在写入配置前拦下。
func TestValidateEndpointRejectsMissingScheme(t *testing.T) {
	bad := []struct {
		name, in string
	}{
		{"bare host", "s3.example.com"},
		{"bare host with port", "s3.example.com:9000"},
		{"garbage", "::::not a url::::"},
		{"unsupported scheme", "ftp://s3.example.com"},
		{"scheme only", "https://"},
		{"empty", ""},
		{"whitespace", "   "},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := ValidateEndpoint(tc.in); err == nil {
				t.Fatalf("ValidateEndpoint(%q) = %q, want an error", tc.in, got)
			}
		})
	}
}

func TestValidateEndpointAcceptsHTTPAndHTTPS(t *testing.T) {
	good := []struct {
		in   string
		want string
	}{
		{"https://s3.example.com", "https://s3.example.com"},
		{"https://s3.example.com:8443", "https://s3.example.com:8443"},
		{"http://127.0.0.1:9000", "http://127.0.0.1:9000"},
		{"  https://s3.example.com  ", "https://s3.example.com"}, // 去空白
		{"https://s3.example.com/base/path", "https://s3.example.com/base/path"},
	}
	for _, tc := range good {
		got, err := ValidateEndpoint(tc.in)
		if err != nil {
			t.Errorf("ValidateEndpoint(%q) returned error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ValidateEndpoint(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestSetAliasStaticRejectsBadEndpointBeforeWriting 确认拒绝发生在落盘之前 ——
// 否则用户的配置文件里会留下一个坏别名。
func TestSetAliasStaticRejectsBadEndpointBeforeWriting(t *testing.T) {
	dir := t.TempDir()
	confPath := filepath.Join(dir, ".s3cli")

	oldS, oldC := G.S, G.C
	t.Cleanup(func() { G.S, G.C = oldS, oldC })
	G.S = map[string]Static{}
	G.C = confPath

	for _, ep := range []string{"s3.example.com", "::::not a url::::", "ftp://x.test"} {
		err := setAliasStatic("ep", ep, "AKIA", "SECRET", "")
		if err == nil {
			t.Fatalf("setAliasStatic(%q) must be rejected", ep)
		}
		if _, statErr := os.Stat(confPath); statErr == nil {
			t.Fatalf("config file was written despite rejecting %q", ep)
		}
		if _, ok := G.S["ep"]; ok {
			t.Fatalf("alias was added to the in-memory table despite rejecting %q", ep)
		}
	}

	// 合法输入仍然写入, 且落到磁盘的值就是规范化后的 host_base。
	if err := setAliasStatic("ep", " https://s3.example.com ", "AKIA", "SECRET", ""); err != nil {
		t.Fatalf("valid endpoint rejected: %v", err)
	}
	data, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `host_base = "https://s3.example.com"`) {
		t.Fatalf("config content = %q", data)
	}
	if got := G.S["ep"].HostBase; got != "https://s3.example.com" {
		t.Fatalf("stored host_base = %q", got)
	}
}
