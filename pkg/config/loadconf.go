package config

import (
	"fmt"
	"os"

	"gopkg.in/ini.v1"
)

func LoadConf() error {
	if ConfigPath == "" {
		ConfigPath = DefaultConfigPath
	}

	// 初始化 G.S（防止 nil map panic）
	if G.S == nil {
		G.S = make(map[string]Static)
	}

	// 文件不存在报错
	info, err := os.Stat(ConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("config file not found: %s", ConfigPath)
		}
		return fmt.Errorf("stat config %s: %w", ConfigPath, err)
	}

	// 文件为空报错
	if info.Size() == 0 {
		return fmt.Errorf("config file is empty: %s", ConfigPath)
	}

	// 解析错误报错
	cfg, err := ini.Load(ConfigPath)
	if err != nil {
		return fmt.Errorf("load config %s: %w", ConfigPath, err)
	}

	sections := cfg.Sections()
	for _, sec := range sections {
		name := sec.Name()
		// 空配置忽略
		if name == ini.DefaultSection && len(sec.Keys()) == 0 {
			continue
		}

		// 解析 section 到临时变量，再写入 map（map 索引不可寻址，必须用指针）
		s := Static{}
		if err := sec.MapTo(&s); err != nil {
			return fmt.Errorf("parse section [%s]: %w", name, err)
		}
		G.S[name] = s
	}
	return nil
}
