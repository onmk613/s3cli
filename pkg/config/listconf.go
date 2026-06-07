package config

import (
	"fmt"
	"os"
	"sort"
	"strings"

	myprint "s3cli/pkg/fmtutil"

	"gopkg.in/ini.v1"
)

func ListAliasConf() error {
	if ConfigPath == "" {
		ConfigPath = DefaultConfigPath
	}

	info, err := os.Stat(ConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("config file not found: %s (run `s3cmd alias set <name>` to create one)", ConfigPath)
		}
		return fmt.Errorf("stat config %s: %w", ConfigPath, err)
	}
	if info.Size() == 0 {
		return fmt.Errorf("config file is empty: %s", ConfigPath)
	}

	cfg, err := ini.Load(ConfigPath)
	if err != nil {
		return fmt.Errorf("load config %s: %w", ConfigPath, err)
	}

	// 收集有效 section（排除空的 DEFAULT 与全部空值的 section）
	type secInfo struct {
		name string
		keys []*ini.Key
	}
	var sections []secInfo
	for _, sec := range cfg.Sections() {
		name := sec.Name()
		keys := sec.Keys()
		if name == ini.DefaultSection && len(keys) == 0 {
			continue
		}
		hasValue := false
		for _, k := range keys {
			if strings.TrimSpace(k.Value()) != "" {
				hasValue = true
				break
			}
		}
		if !hasValue {
			continue
		}
		sections = append(sections, secInfo{name: name, keys: keys})
	}

	if len(sections) == 0 {
		myprint.PrintlnYellow("no aliases configured.")
		myprint.Println("Hint: run `s3cmd alias set <name>` to create one.")
		return nil
	}

	// DEFAULT 优先，其他按名字排序
	sort.Slice(sections, func(i, j int) bool {
		if sections[i].name == ini.DefaultSection {
			return true
		}
		if sections[j].name == ini.DefaultSection {
			return false
		}
		return sections[i].name < sections[j].name
	})

	myprint.PrintfDim("Config:")
	myprint.Printf(" %s\n", ConfigPath)
	myprint.Println()

	for i, s := range sections {
		// 标题：[alias_name]
		myprint.PrintfBoldCyan("[%s]\n", s.name)

		// 只展示核心字段：URL、AK、SK
		coreKeys := map[string]bool{
			"host_base":  true,
			"access_key": true,
			"secret_key": true,
		}
		for _, k := range s.keys {
			if !coreKeys[k.Name()] {
				continue
			}
			val := strings.TrimSpace(k.Value())
			if val == "" {
				continue
			}
			myprint.Printf("  ")
			myprint.PrintfGreen("%s", k.Name())
			myprint.PrintfDim(" = ")
			myprint.PrintfYellow("%s\n", val)
		}

		if i != len(sections)-1 {
			myprint.Println()
		}
	}
	return nil
}
