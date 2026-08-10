package config

import (
	"bufio"
	"errors"
	"strings"
	"testing"
)

func TestDelConf(t *testing.T) {
	path := tempConfPath(t)
	G.S = map[string]Static{
		"a": {HostBase: "https://a", AccessKey: "ak", SecretKey: "sk"},
		"b": {HostBase: "https://b", AccessKey: "ak", SecretKey: "sk"},
	}
	if err := saveConfig(path); err != nil {
		t.Fatal(err)
	}

	// 非交互（默认 isTerminal=false）→ 直接删除
	if err := DelConf([]string{"a"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := G.S["a"]; ok {
		t.Error("alias a should be deleted")
	}
	if _, ok := G.S["b"]; !ok {
		t.Error("alias b should remain")
	}

	// 删除不存在的别名 → 报错, 且已存在的部分仍被删除
	if err := DelConf([]string{"b", "ghost"}); err == nil {
		t.Error("partial failure should error")
	}
	if _, ok := G.S["b"]; ok {
		t.Error("alias b should be deleted despite later failure")
	}

	// 多个全部成功
	G.S = map[string]Static{
		"a": {HostBase: "https://a", AccessKey: "ak", SecretKey: "sk"},
		"b": {HostBase: "https://b", AccessKey: "ak", SecretKey: "sk"},
	}
	if err := saveConfig(path); err != nil {
		t.Fatal(err)
	}
	if err := DelConf([]string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	if len(G.S) != 0 {
		t.Errorf("all aliases should be deleted, got %v", G.S)
	}
}

func TestDelConfInteractive(t *testing.T) {
	snapshotHooks(t)
	path := tempConfPath(t)
	G.S = map[string]Static{
		"a": {HostBase: "https://a", AccessKey: "ak", SecretKey: "sk"},
		"b": {HostBase: "https://b", AccessKey: "ak", SecretKey: "sk"},
	}
	if err := saveConfig(path); err != nil {
		t.Fatal(err)
	}
	isTerminal = func(int) bool { return true }

	// y 确认删除 a, n 跳过 b
	feedStdin(t, "y\nn\n")
	if err := DelConf([]string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := G.S["a"]; ok {
		t.Error("a should be deleted after confirm")
	}
	if _, ok := G.S["b"]; !ok {
		t.Error("b should be skipped after n")
	}

	// stdin 无输入（EOF）→ 全部跳过
	feedStdin(t, "")
	if err := DelConf([]string{"b"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := G.S["b"]; !ok {
		t.Error("b should survive EOF confirm")
	}
}

func TestConfirmDelete(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"y\n", true},
		{"yes\n", true},
		{"Y\n", true},
		{"YES\n", true},
		{"n\n", false},
		{"\n", false},
		{"maybe\n", false},
		{"", false}, // EOF
	}
	for _, tc := range cases {
		got := confirmDelete(bufio.NewReader(strings.NewReader(tc.in)), "x", "path")
		if got != tc.want {
			t.Errorf("confirmDelete(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestDelConfUnit(t *testing.T) {
	path := tempConfPath(t)

	// readConfig 失败（文件不存在）
	if err := delConf("a", path); !errors.Is(err, ErrConfigNotFoundOrEmpty) {
		t.Errorf("want ErrConfigNotFoundOrEmpty, got %v", err)
	}

	// alias 不存在
	G.S = map[string]Static{"x": {HostBase: "https://x", AccessKey: "ak", SecretKey: "sk"}}
	if err := saveConfig(path); err != nil {
		t.Fatal(err)
	}
	if err := delConf("ghost", path); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("want not-found error, got %v", err)
	}

	// 成功删除
	if err := delConf("x", path); err != nil {
		t.Fatal(err)
	}
	if _, ok := G.S["x"]; ok {
		t.Error("alias x should be deleted")
	}
}

func TestDelConfSaveError(t *testing.T) {
	snapshotHooks(t)
	path := tempConfPath(t)
	G.S = map[string]Static{"a": {HostBase: "https://a", AccessKey: "ak", SecretKey: "sk"}}
	if err := saveConfig(path); err != nil {
		t.Fatal(err)
	}
	rename = func(string, string) error { return errors.New("rename") }
	if err := delConf("a", path); err == nil {
		t.Error("want save error")
	}
}
