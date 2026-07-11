package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	myprint "s3cli/pkg/fmtutil"

	"gopkg.in/ini.v1"
)

// DelConf 从配置文件中删除指定别名 section。
// 删除前会要求用户输入 y/Y 进行确认。
func DelConf(sections []string) error {
	if ConfPath == "" {
		ConfPath = DefaultConfigPath
	}

	if _, err := os.Stat(ConfPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("config file not found: %s", ConfPath)
		}
		return fmt.Errorf("stat config %s: %w", ConfPath, err)
	}

	var errs []error
	for _, section := range sections {
		if err := delConf(section); err != nil {
			myprint.PrintfRed("%s\n", err)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func delConf(section string) error {
	section = strings.TrimSpace(section)
	if section == "" {
		return fmt.Errorf("alias name cannot be empty")
	}

	cfg, err := ini.Load(ConfPath)
	if err != nil {
		return fmt.Errorf("load config %s: %w", ConfPath, err)
	}

	// ini.DefaultSection 名为 "DEFAULT"，禁止显式删除
	if strings.EqualFold(section, ini.DefaultSection) {
		return fmt.Errorf("cannot delete the [%s] section", ini.DefaultSection)
	}

	if !cfg.HasSection(section) {
		return fmt.Errorf("alias [%s] not found in %s", section, ConfPath)
	}

	cfg.DeleteSection(section)
	if err := cfg.SaveTo(ConfPath); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	myprint.PrintfGreen("Alias [%s] deleted from %s\n", section, ConfPath)
	return nil
}
