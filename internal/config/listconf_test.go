package config

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	myprint "s3cli/pkg/fmtutil"
)

// captureOutput 重定向 myprint 输出并返回可读缓冲区。
func captureOutput(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	myprint.SetWriter(&buf)
	t.Cleanup(func() { myprint.SetWriter(os.Stdout) })
	return &buf
}

func TestListAliasConf(t *testing.T) {
	path := tempConfPath(t)
	buf := captureOutput(t)

	// 无配置文件 → ErrConfigNotFoundOrEmpty
	if err := ListAliasConf(nil); !errors.Is(err, ErrConfigNotFoundOrEmpty) {
		t.Errorf("want ErrConfigNotFoundOrEmpty, got %v", err)
	}

	// 全空 alias（core 字段全空）→ 被过滤, 提示 no aliases
	G.S = map[string]Static{"empty": {}}
	if err := saveConfig(path); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	if err := ListAliasConf(nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "no aliases configured") {
		t.Errorf("want empty hint, got %q", buf.String())
	}

	// 正常列出：排序 + secret 脱敏 + 空字段跳过
	G.S = map[string]Static{
		"zeta":  {HostBase: "https://z", AccessKey: "zak", SecretKey: "zsecretkey"},
		"alpha": {HostBase: "https://a", AccessKey: "aak", SecretKey: ""}, // secret 为空 → 字段跳过
	}
	if err := saveConfig(path); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	if err := ListAliasConf(nil); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Index(out, "[alpha]") > strings.Index(out, "[zeta]") {
		t.Errorf("aliases should be sorted, got %q", out)
	}
	if !strings.Contains(out, "****tkey") {
		t.Errorf("secret should be masked, got %q", out)
	}
	if strings.Contains(out, "zsecretkey") {
		t.Errorf("plaintext secret leaked: %q", out)
	}
	// 空 secret 字段被跳过: 只有 zeta 输出一行 secret_key
	if n := strings.Count(out, "secret_key = "); n != 1 {
		t.Errorf("secret_key should appear exactly once (zeta only), got %d: %q", n, out)
	}

	// 过滤：只列指定 alias
	buf.Reset()
	if err := ListAliasConf([]string{"zeta"}); err != nil {
		t.Fatal(err)
	}
	out = buf.String()
	if !strings.Contains(out, "[zeta]") || strings.Contains(out, "[alpha]") {
		t.Errorf("filter should keep only zeta, got %q", out)
	}

	// ShowSecret → 明文
	G.F.ShowSecret = true
	buf.Reset()
	if err := ListAliasConf(nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "zsecretkey") {
		t.Errorf("show-secret should reveal plaintext, got %q", buf.String())
	}
}

func TestMaskSecret(t *testing.T) {
	cases := map[string]string{
		"":         "****",
		"abcd":     "****",
		"abcdefgh": "****efgh",
		"密钥尾部多字节":  "****部多字节", // 按 rune 截断, 不切坏 UTF-8
	}
	for in, want := range cases {
		if got := maskSecret(in); got != want {
			t.Errorf("maskSecret(%q) = %q, want %q", in, got, want)
		}
	}
}
