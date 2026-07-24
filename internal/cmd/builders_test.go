package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

// TestAllCommandBuildersConstruct 遍历注册表, 构造每一个顶层命令,
// 覆盖所有 New*Cmd 构造逻辑 (flag 注册 / ValidArgs / Args 校验等)。
// 不执行 RunE (那需要网络/配置)。
func TestAllCommandBuildersConstruct(t *testing.T) {
	for _, g := range cmdRegistry {
		for _, fn := range g.Commands {
			cmd := fn()
			if cmd == nil {
				t.Errorf("group %q: factory returned nil", g.ID)
				continue
			}
			if cmd.Use == "" {
				t.Errorf("group %q: command has empty Use", g.ID)
			}
			// 解析 flags 确保注册逻辑执行
			_ = cmd.Flags()
			_ = cmd.PersistentFlags()
			// 验证 Args 校验器存在时对零参数的判定不 panic
			if cmd.Args != nil {
				_ = cmd.Args(cmd, nil)
			}
		}
	}
}

// TestCommandsHaveExpectedShape 抽查几个关键命令的结构。
func TestCommandsHaveExpectedShape(t *testing.T) {
	type want struct {
		use   string
		alias []string
		flags []string
	}
	cases := []struct {
		name string
		cmd  *cobra.Command
		want want
	}{
		{"ls", NewLsCmd(), want{use: "ls", alias: []string{"list", "l"}}},
		{"bucket", NewBucketCmd(), want{use: "bucket"}},
		{"find", NewFindCmd(), want{use: "find", flags: []string{"name", "regex"}}},
		{"tree", NewTreeCmd(), want{use: "tree", flags: []string{"size"}}},
		{"share", NewShareCmd(), want{use: "signurl", flags: []string{"expire"}}},
		{"alias", NewAliasCmd(), want{use: "alias"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.cmd.Use == "" {
				t.Error("empty Use")
			}
			for _, a := range tc.want.alias {
				if !containsStr(tc.cmd.Aliases, a) {
					t.Errorf("missing alias %q in %v", a, tc.cmd.Aliases)
				}
			}
			for _, f := range tc.want.flags {
				if tc.cmd.Flags().Lookup(f) == nil {
					t.Errorf("missing flag %q", f)
				}
			}
		})
	}
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
