package config

import (
	"fmt"
	"os"

	"gopkg.in/ini.v1"
)

// LoadConf 读取配置文件，解析为全局变量 G.S。
// 如果配置文件不存在或为空，返回错误。
// 如果配置文件中有无效的 section，返回错误。
func LoadConf() error {
	ensureConfPath()

	info, err := os.Stat(ConfPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("config file not found: %s", ConfPath)
		}
		return fmt.Errorf("stat config %s: %w", ConfPath, err)
	}

	if info.Size() == 0 {
		return fmt.Errorf("config file is empty: %s", ConfPath)
	}

	cfg, err := ini.Load(ConfPath)
	if err != nil {
		return fmt.Errorf("load config %s: %w", ConfPath, err)
	}

	sections := cfg.Sections()
	newS := make(map[string]Static, len(sections))
	for _, sec := range sections {
		name := sec.Name()

		if name == ini.DefaultSection && len(sec.Keys()) == 0 {
			continue
		}

		s := Static{}
		if err := sec.MapTo(&s); err != nil {
			return fmt.Errorf("parse section [%s]: %w", name, err)
		}

		if !sec.HasKey("verify_ssl") {
			s.VerifySSL = true
		}
		newS[name] = s
	}

	G.S = newS
	return nil
}
