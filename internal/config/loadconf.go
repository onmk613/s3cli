package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// loadRaw 把配置文件原样反序列化为 map[name]Static，并返回 TOML MetaData
// 用于 IsDefined 判断（区分 "键不存在" 与 "键显式等于零值"，例如 verify_ssl）。
// 调用方负责校验文件存在性与大小。
func loadRaw() (map[string]Static, toml.MetaData, error) {
	m := make(map[string]Static)
	md, err := toml.DecodeFile(ConfPath, &m)
	if err != nil {
		return nil, toml.MetaData{}, err
	}
	return m, md, nil
}

// LoadConf 读取配置文件，解析为全局变量 G.S。
// 如果配置文件不存在或为空，返回错误。
// 配置中省略 verify_ssl 的别名会被视为 true（与原 INI 版本语义一致）。
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

	m, md, err := loadRaw()
	if err != nil {
		return fmt.Errorf("load config %s: %w", ConfPath, err)
	}

	// 省略 verify_ssl 时默认 true（TOML 反序列化会留下 bool 零值 false，
	// 必须借助 MetaData 判断键是否出现过，再回填）。
	for name, s := range m {
		if !md.IsDefined(name, "verify_ssl") {
			s.VerifySSL = true
			m[name] = s
		}
	}

	G.S = m
	return nil
}
