package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	myprint "s3cli/pkg/fmtutil"

	"golang.org/x/term"
	"gopkg.in/ini.v1"
)

// DelConf 从配置文件中删除指定别名 section。
// stdin 是终端时逐个要求输入 y/Y 确认 (别名含密钥, 误删不可逆);
// 非终端 (脚本/管道) 直接删除, 保持自动化可用。
func DelConf(sections []string) error {
	ensureConfPath()

	if _, err := os.Stat(ConfPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("config file not found: %s", ConfPath)
		}
		return fmt.Errorf("stat config %s: %w", ConfPath, err)
	}

	interactive := term.IsTerminal(int(os.Stdin.Fd()))
	reader := bufio.NewReader(os.Stdin)

	var errs []error
	for _, section := range sections {
		if interactive && !confirmDelete(reader, section) {
			myprint.PrintfYellow("skip [%s]: not confirmed\n", section)
			continue
		}
		if err := delConf(section); err != nil {
			myprint.PrintfRed("%s\n", err)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// confirmDelete 终端交互确认删除, 仅 y/Y 视为确认。
func confirmDelete(reader *bufio.Reader, section string) bool {
	myprint.Printf("Delete alias [%s] from %s? (y/N): ", section, ConfPath)
	ans, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	ans = strings.ToLower(strings.TrimSpace(ans))
	return ans == "y" || ans == "yes"
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
	if err := saveConfig(cfg, ConfPath); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	myprint.PrintfGreen("Alias [%s] deleted from %s\n", section, ConfPath)
	return nil
}
