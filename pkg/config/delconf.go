package config

import (
	"fmt"
	"os"
	"strings"

	myprint "s3cli/pkg/fmtutil"

	"gopkg.in/ini.v1"
)

// DelConf 从配置文件中删除指定别名 section。
// 删除前会要求用户输入 y/Y 进行确认。
func DelConf(sections []string) error {
	if ConfigPath == "" {
		ConfigPath = DefaultConfigPath
	}

	if _, err := os.Stat(ConfigPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("config file not found: %s", ConfigPath)
		}
		return fmt.Errorf("stat config %s: %w", ConfigPath, err)
	}

	for _, section := range sections {
		if err := delConf(section); err != nil {
			fmt.Errorf(err.Error())
		}
	}
	return nil
}

func delConf(section string) error {
	section = strings.TrimSpace(section)
	if section == "" {
		return fmt.Errorf("alias name cannot be empty")
	}

	cfg, err := ini.Load(ConfigPath)
	if err != nil {
		return fmt.Errorf("load config %s: %w", ConfigPath, err)
	}

	// ini.DefaultSection 名为 "DEFAULT"，禁止显式删除
	if strings.EqualFold(section, ini.DefaultSection) {
		return fmt.Errorf("cannot delete the [%s] section", ini.DefaultSection)
	}

	if !cfg.HasSection(section) {
		return fmt.Errorf("alias [%s] not found in %s", section, ConfigPath)
	}

	cfg.DeleteSection(section)
	if err := cfg.SaveTo(ConfigPath); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	myprint.PrintfGreen("Alias [%s] deleted from %s\n", section, ConfigPath)
	return nil
}
