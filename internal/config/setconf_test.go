package config

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

func TestSetAliasConfArgs(t *testing.T) {
	path := tempConfPath(t)

	// 0 / 2 / 3 / 6 个参数 → 报错
	for _, args := range [][]string{nil, {"a", "b"}, {"a", "b", "c"}, {"a", "b", "c", "d", "e", "f"}} {
		if err := SetAliasConf(context.Background(), args); err == nil {
			t.Errorf("SetAliasConf(%v) should error", args)
		}
	}

	// 4 个参数 → 非交互写入
	if err := SetAliasConf(context.Background(), []string{"t", "https://h", "ak", "sk"}); err != nil {
		t.Fatal(err)
	}
	got := G.S["t"]
	if got.HostBase != "https://h" || got.AccessKey != "ak" || got.SecretKey != "sk" || got.SessionToken != "" {
		t.Errorf("unexpected alias: %+v", got)
	}

	// 5 个参数 → 带 session token
	if err := SetAliasConf(context.Background(), []string{"t", "https://h", "ak", "sk", "tok"}); err != nil {
		t.Fatal(err)
	}
	if G.S["t"].SessionToken != "tok" {
		t.Errorf("session token not saved: %+v", G.S["t"])
	}

	// 覆盖已有 alias 时, 未交互字段 (region / max_retries) 通过值拷贝保留
	G.S = map[string]Static{"t": {HostBase: "x", AccessKey: "a", SecretKey: "s", Region: "us-east-1", MaxRetries: 7}}
	if err := saveConfig(path); err != nil {
		t.Fatal(err)
	}
	if err := SetAliasConf(context.Background(), []string{"t", "https://h2", "ak2", "sk2"}); err != nil {
		t.Fatal(err)
	}
	got = G.S["t"]
	if got.HostBase != "https://h2" || got.Region != "us-east-1" || got.MaxRetries != 7 {
		t.Errorf("overwrite should preserve untouched fields: %+v", got)
	}
}

// TestSetAliasStaticValidation 非交互路径应对必填字段做 trim + 非空校验,
// 空凭证不再静默落盘。
func TestSetAliasStaticValidation(t *testing.T) {
	tempConfPath(t)

	for _, args := range [][]string{
		{"  ", "https://h", "ak", "sk"}, // 空别名
		{"a", "  ", "ak", "sk"},         // 空 host base
		{"a", "https://h", "  ", "sk"},  // 空 access key
		{"a", "https://h", "ak", "  "},  // 空 secret key
	} {
		if err := SetAliasConf(context.Background(), args); err == nil {
			t.Errorf("SetAliasConf(%q) should error", args)
		}
	}

	// 值两侧空白应被 trim 后写入
	if err := SetAliasConf(context.Background(), []string{"t", " https://h ", " ak ", " sk ", " tok "}); err != nil {
		t.Fatal(err)
	}
	got := G.S["t"]
	if got.HostBase != "https://h" || got.AccessKey != "ak" || got.SecretKey != "sk" || got.SessionToken != "tok" {
		t.Errorf("fields not trimmed: %+v", got)
	}
}

func TestSetAliasInteractive(t *testing.T) {
	path := tempConfPath(t)

	// 无配置文件 → 交互创建全新 alias（8 个字段输入）
	input := "https://new.example.com\nnewak\nnewsk\n\n\n\ntrue\n64\n"
	feedStdin(t, input)
	if err := SetAliasConf(context.Background(), []string{"brand"}); err != nil {
		t.Fatal(err)
	}
	want := Static{
		HostBase: "https://new.example.com", AccessKey: "newak", SecretKey: "newsk",
		NoVerifySSL: true, MultipartChunkSizeMb: 64,
	}
	if G.S["brand"] != want {
		t.Errorf("got %+v, want %+v", G.S["brand"], want)
	}

	// 已存在 alias → 回车保留旧值
	G.S = map[string]Static{"brand": {HostBase: "https://old", AccessKey: "oldak", SecretKey: "oldsk", Region: "us-west-2"}}
	if err := saveConfig(path); err != nil {
		t.Fatal(err)
	}
	feedStdin(t, "\n\n\n\n\n\n\n\n") // 全部回车保留
	if err := SetAliasConf(context.Background(), []string{"brand"}); err != nil {
		t.Fatal(err)
	}
	want = Static{HostBase: "https://old", AccessKey: "oldak", SecretKey: "oldsk", Region: "us-west-2"}
	if G.S["brand"] != want {
		t.Errorf("got %+v, want %+v", G.S["brand"], want)
	}
}

func TestEditAliasConf(t *testing.T) {
	path := tempConfPath(t)
	G.S = map[string]Static{"my": {HostBase: "https://old", AccessKey: "ak", SecretKey: "sk", Region: "us-west-2", MaxRetries: 3}}
	if err := saveConfig(path); err != nil {
		t.Fatal(err)
	}

	// host 保留 / ak sk 更新 / token 用 '-' 清空 / region 保留 / lookup 保留 / verify false / chunk 0
	input := "\nnewak\nnewsk\n-\n\n\nfalse\n0\n"
	feedStdin(t, input)
	if err := EditAliasConf(context.Background(), "my"); err != nil {
		t.Fatal(err)
	}
	want := Static{
		HostBase: "https://old", AccessKey: "newak", SecretKey: "newsk",
		Region: "us-west-2", MaxRetries: 3,
	}
	if G.S["my"] != want {
		t.Errorf("got %+v, want %+v", G.S["my"], want)
	}
}

func TestEditAliasConfErrors(t *testing.T) {
	snapshotHooks(t)
	path := tempConfPath(t)

	// 空名称
	if err := EditAliasConf(context.Background(), "  "); err == nil || !strings.Contains(err.Error(), "cannot be empty") {
		t.Errorf("want empty-name error, got %v", err)
	}

	// 配置文件不存在
	if err := EditAliasConf(context.Background(), "ghost"); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("want missing-config error, got %v", err)
	}

	// alias 不存在
	G.S = map[string]Static{"other": {HostBase: "https://x", AccessKey: "a", SecretKey: "s"}}
	if err := saveConfig(path); err != nil {
		t.Fatal(err)
	}
	if err := EditAliasConf(context.Background(), "ghost"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("want not-found error, got %v", err)
	}

	// saveConfig 失败 → "save config: ..."
	G.S = map[string]Static{"my": {HostBase: "https://old", AccessKey: "ak", SecretKey: "sk"}}
	if err := saveConfig(path); err != nil {
		t.Fatal(err)
	}
	rename = func(string, string) error { return errors.New("rename boom") }
	feedStdin(t, "\n\n\n\n\n\n\n\n") // 全部保留
	if err := EditAliasConf(context.Background(), "my"); err == nil || !strings.Contains(err.Error(), "save config") {
		t.Errorf("want save-config error, got %v", err)
	}
}

func TestInteractEdit(t *testing.T) {
	// 正常全流程（8 个字段输入, 后 5 行为空）
	feedStdin(t, "https://h\nak\nsk\n\n\n\n\n\n") // 全字段输入, 第 4-8 行为空
	conf, err := interactEdit(context.Background(), Static{})
	if err != nil {
		t.Fatal(err)
	}
	want := Static{HostBase: "https://h", AccessKey: "ak", SecretKey: "sk"}
	if conf != want {
		t.Errorf("got %+v, want %+v", conf, want)
	}
}

func TestInteractEditRetries(t *testing.T) {
	// host/ak/sk 空输入重试, lookup 非法重试, verify 非法重试, chunk 非法重试
	input := "\nhttps://h\n\nak\n\nsk\n\n\nbad\n\ndns\nabc\ntrue\nxyz\n32\n"
	feedStdin(t, input)
	conf, err := interactEdit(context.Background(), Static{})
	if err != nil {
		t.Fatal(err)
	}
	want := Static{
		HostBase: "https://h", AccessKey: "ak", SecretKey: "sk",
		BucketLookup: "dns", NoVerifySSL: true, MultipartChunkSizeMb: 32,
	}
	if conf != want {
		t.Errorf("got %+v, want %+v", conf, want)
	}
}

func TestInteractEditInterrupted(t *testing.T) {
	// stdin 立即 EOF → errInterrupted
	feedStdin(t, "")
	if _, err := interactEdit(context.Background(), Static{}); !errors.Is(err, errInterrupted) {
		t.Errorf("want errInterrupted, got %v", err)
	}

	// ctx 已取消 → errInterrupted（stdin 管道关闭避免 goroutine 阻塞）
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	feedStdin(t, "")
	if _, err := interactEdit(ctx, Static{}); !errors.Is(err, errInterrupted) {
		t.Errorf("want errInterrupted, got %v", err)
	}
}

func TestInteractEditReadError(t *testing.T) {
	// stdin 指向已关闭的文件 → ReadString 返回非 EOF 错误 → "read input: ..."
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = f
	t.Cleanup(func() { os.Stdin = old })
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := interactEdit(context.Background(), Static{}); err == nil || !strings.Contains(err.Error(), "read input") {
		t.Errorf("want read-input error, got %v", err)
	}
}

func TestInteractEditSecretTerminal(t *testing.T) {
	snapshotHooks(t)
	isTerminal = func(int) bool { return true }

	t.Run("read password ok", func(t *testing.T) {
		snapshotHooks(t)
		readPassword = func(int) ([]byte, error) { return []byte("secret"), nil }
		feedStdin(t, "https://h\nak\n\n\n\nfalse\n0\n")
		conf, err := interactEdit(context.Background(), Static{SecretKey: "old"})
		if err != nil {
			t.Fatal(err)
		}
		if conf.SecretKey != "secret" {
			t.Errorf("secret key = %q, want stub value", conf.SecretKey)
		}
	})

	t.Run("empty password keeps default", func(t *testing.T) {
		snapshotHooks(t)
		readPassword = func(int) ([]byte, error) { return []byte("  \n"), nil }
		feedStdin(t, "https://h\nak\n\n\n\nfalse\n0\n")
		conf, err := interactEdit(context.Background(), Static{SecretKey: "old"})
		if err != nil {
			t.Fatal(err)
		}
		if conf.SecretKey != "old" {
			t.Errorf("secret key = %q, want old value kept", conf.SecretKey)
		}
	})

	t.Run("read password eof", func(t *testing.T) {
		snapshotHooks(t)
		readPassword = func(int) ([]byte, error) { return nil, io.EOF }
		feedStdin(t, "https://h\nak\n\n\n\nfalse\n0\n")
		if _, err := interactEdit(context.Background(), Static{}); !errors.Is(err, errInterrupted) {
			t.Errorf("want errInterrupted, got %v", err)
		}
	})

	t.Run("read password error", func(t *testing.T) {
		snapshotHooks(t)
		readPassword = func(int) ([]byte, error) { return nil, errors.New("boom") }
		feedStdin(t, "https://h\nak\n\n\n\nfalse\n0\n")
		if _, err := interactEdit(context.Background(), Static{}); err == nil || !strings.Contains(err.Error(), "read secret") {
			t.Errorf("want read-secret error, got %v", err)
		}
	})
}

func TestInteractEditInterruptAtField(t *testing.T) {
	// 每个字段的读取错误路径: 输入前 n 个字段后 EOF, 下一个字段读取返回 errInterrupted。
	cases := []struct {
		name  string
		lines []string
	}{
		{"host", nil},
		{"access key", []string{"https://h"}},
		{"secret key", []string{"https://h", "ak"}},
		{"session token", []string{"https://h", "ak", "sk"}},
		{"region", []string{"https://h", "ak", "sk", ""}},
		{"bucket lookup", []string{"https://h", "ak", "sk", "", ""}},
		{"no verify ssl", []string{"https://h", "ak", "sk", "", "", ""}},
		{"chunk size", []string{"https://h", "ak", "sk", "", "", "", ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := ""
			if len(tc.lines) > 0 {
				input = strings.Join(tc.lines, "\n") + "\n"
			}
			feedStdin(t, input)
			if _, err := interactEdit(context.Background(), Static{}); !errors.Is(err, errInterrupted) {
				t.Errorf("want errInterrupted, got %v", err)
			}
		})
	}
}

func TestInteractEditEOFWithPartialLine(t *testing.T) {
	// 管道末尾无换行的数据应被接受（EOF+数据分支），随后下一个字段读到 EOF 中断。
	feedStdin(t, "https://h\nak\nsk") // sk 行无换行
	if _, err := interactEdit(context.Background(), Static{}); !errors.Is(err, errInterrupted) {
		t.Fatalf("want errInterrupted after partial line, got %v", err)
	}
}

func TestSetAliasInteractiveErrors(t *testing.T) {
	snapshotHooks(t)
	tempConfPath(t)

	// readConfig 返回非 NotFound 错误
	osStat = func(string) (os.FileInfo, error) { return nil, errors.New("boom") }
	if err := SetAliasConf(context.Background(), []string{"x"}); err == nil || err.Error() != "boom" {
		t.Errorf("want stat error, got %v", err)
	}

	// 交互中断
	osStat = os.Stat
	feedStdin(t, "")
	if err := SetAliasConf(context.Background(), []string{"x"}); !errors.Is(err, errInterrupted) {
		t.Errorf("want errInterrupted, got %v", err)
	}
}

func TestEditAliasConfExtra(t *testing.T) {
	snapshotHooks(t)
	path := tempConfPath(t)
	G.S = map[string]Static{"my": {HostBase: "https://old", AccessKey: "ak", SecretKey: "sk"}}
	if err := saveConfig(path); err != nil {
		t.Fatal(err)
	}

	// ctx == nil → 使用 Background
	feedStdin(t, "\n\n\n\n\n\n\n\n")
	if err := EditAliasConf(context.TODO(), "my"); err != nil {
		t.Fatal(err)
	}

	// readConfig 返回非 NotFound 错误
	osStat = func(string) (os.FileInfo, error) { return nil, errors.New("boom") }
	if err := EditAliasConf(context.Background(), "my"); err == nil || err.Error() != "boom" {
		t.Errorf("want stat error, got %v", err)
	}

	// 交互中断
	osStat = os.Stat
	feedStdin(t, "")
	if err := EditAliasConf(context.Background(), "my"); !errors.Is(err, errInterrupted) {
		t.Errorf("want errInterrupted, got %v", err)
	}
}

func TestSetAliasStaticReadError(t *testing.T) {
	snapshotHooks(t)
	tempConfPath(t)
	osStat = func(string) (os.FileInfo, error) { return nil, errors.New("boom") }
	if err := SetAliasConf(context.Background(), []string{"t", "https://h", "ak", "sk"}); err == nil || err.Error() != "boom" {
		t.Errorf("want stat error, got %v", err)
	}
}

func TestInteractEditGoroutineCtxExit(t *testing.T) {
	// goroutine 发送侧的 ctx.Done 退出分支与发送分支在 select 中随机竞争,
	// 单次运行无法保证命中。多次尝试以覆盖两条路径（正常数据行与带数据 EOF）。
	run := func(input string) {
		ctx, cancel := context.WithCancel(context.Background())
		pr, pw, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		old := os.Stdin
		os.Stdin = pr
		go func() {
			_, _ = pw.WriteString(input)
			_ = pw.Close()
		}()
		cancel()
		_, _ = interactEdit(ctx, Static{})
		os.Stdin = old
		_ = pr.Close() // 触发 goroutine 的 ReadString 返回并退出
	}
	for i := 0; i < 60; i++ {
		run("https://h\nak\nsk\n") // 正常路径的发送 select
		run("https://h\nak\nsk")   // 带数据 EOF 的错误发送 select
	}
}
