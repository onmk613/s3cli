package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	myprint "s3cli/pkg/fmtutil"
	"strings"
)

// DelConf 从配置文件中删除指定别名 section (alias del)。
// stdin 是终端时逐个要求输入 y/Y 确认 (别名含密钥, 误删不可逆);
// 非终端 (脚本/管道) 直接删除, 保持自动化可用。
func DelConf(sections []string) error {
	interactive := isTerminal(int(os.Stdin.Fd()))
	reader := bufio.NewReader(os.Stdin)

	var errs []error
	for _, section := range sections {
		if interactive && !confirmDelete(reader, section, G.C) {
			myprint.PrintfYellow("skip [%s]: not confirmed\n", section)
			continue
		}
		if err := delConf(section, G.C); err != nil {
			myprint.PrintfRed("%s\n", err)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// confirmDelete 终端交互确认删除, 仅 y/Y 视为确认。
func confirmDelete(reader *bufio.Reader, section, confPath string) bool {
	myprint.Printf("Delete alias [%s] from %s? (y/N): ", section, confPath)
	ans, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	ans = strings.ToLower(strings.TrimSpace(ans))
	return ans == "y" || ans == "yes"
}

// delConf 删除配置文件中的指定别名 section。
func delConf(section, confPath string) error {
	if err := readConfig(confPath); err != nil {
		return err
	}

	if _, ok := G.S[section]; !ok {
		return fmt.Errorf("alias [%s] not found in %s", section, confPath)
	}

	delete(G.S, section)
	if err := saveConfig(confPath); err != nil {
		return err
	}

	myprint.PrintfGreen("Alias [%s] deleted from %s\n", section, confPath)
	return nil
}
