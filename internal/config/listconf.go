package config

import (
	"fmt"

	myprint "s3cli/pkg/fmtutil"
	"slices"
	"sort"
	"strings"
)

// ListAliasConf 列出配置文件中的所有别名 (alias list)。
// alias 非空时只列出指定名称的别名。
func ListAliasConf(alias []string) error {
	// 读取配置文件
	if err := readConfig(G.C); err != nil {
		return err
	}

	// 过滤掉 core 字段全空的别名 (等价于 INI 版本里 "跳过空 section" 的逻辑),
	// 再按名字排序输出。
	type entry struct {
		name string
		s    Static
	}
	var entries []entry
	for name, s := range G.S {
		if strings.TrimSpace(s.HostBase) == "" &&
			strings.TrimSpace(s.AccessKey) == "" &&
			strings.TrimSpace(s.SecretKey) == "" {
			continue
		}
		entries = append(entries, entry{name: name, s: s})
	}

	if len(entries) == 0 {
		myprint.PrintlnYellow("no aliases configured.")
		myprint.Println("Hint: run `s3cli alias set <name> URL ACCESSKEY SECRETKEY` to create one.")
		return nil
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].name < entries[j].name
	})

	myprint.PrintfDim("Config:")
	myprint.Printf(" %s\n", G.C)
	myprint.Println()

	// 显式指定了别名但一个都不存在时, 静默成功 (exit 0) 会让脚本误以为列出了配置。
	found := make(map[string]bool, len(alias))
	shown := 0
	for i, e := range entries {
		if len(alias) > 0 && !slices.Contains(alias, e.name) {
			continue
		}
		found[e.name] = true
		shown++

		// 标题：[alias_name]
		myprint.PrintfBoldCyan("[%s]\n", e.name)

		// 只展示核心字段：URL、AK、SK
		type field struct {
			key string
			val string
		}
		fields := []field{
			{"host_base", e.s.HostBase},
			{"access_key", e.s.AccessKey},
			{"secret_key", e.s.SecretKey},
		}
		for _, f := range fields {
			val := strings.TrimSpace(f.val)
			if val == "" {
				continue
			}
			if f.key == "secret_key" && !G.F.ShowSecret {
				val = maskSecret(val)
			}
			myprint.Printf("  ")
			myprint.PrintfGreen("%s", f.key)
			myprint.PrintfDim(" = ")
			myprint.PrintfYellow("%s\n", val)
		}

		if i != len(entries)-1 {
			myprint.Println()
		}
	}
	// 指定的别名一个都没匹配到时明确报错, 而不是静默成功 (exit 0)。
	if len(alias) > 0 && shown == 0 {
		return fmt.Errorf("alias not found: %s (run `s3cli alias list` to see configured aliases)", strings.Join(alias, ", "))
	}
	return nil
}

// maskSecret 把密钥打码为 "****尾4位" (长度 <= 4 时全打码)。
func maskSecret(s string) string {
	r := []rune(s)
	if len(r) <= 4 {
		return "****"
	}
	return "****" + string(r[len(r)-4:])
}
