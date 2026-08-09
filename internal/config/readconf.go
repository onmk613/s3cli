package config

import (
	"errors"
	"os"

	"github.com/BurntSushi/toml"
)

// osStat 可注入钩子（测试用）。
var osStat = os.Stat

// LoadConf 读取配置文件，解析为全局变量 G.S
// 如果配置文件不存在或为空，返回 ErrConfigNotFoundOrEmpty。
func LoadConf() error {
	return readConfig(G.C)
}

// ErrConfigNotFoundOrEmpty 表示配置文件不存在或为空
var ErrConfigNotFoundOrEmpty = errors.New("config file not found or empty")

// readConfig 读取配置文件，解析为 map[string]Static。
// TOML 反序列化会留下零值（如 multipart_chunk_size_mb=0），
// 调用方以 "0 / 空串 即默认值" 的约定处理，无需在此回填。
func readConfig(confPath string) error {
	// 文件是否存在
	info, err := osStat(confPath)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrConfigNotFoundOrEmpty
		}
		return err
	}

	if info.Size() == 0 {
		return ErrConfigNotFoundOrEmpty
	}

	// 解析到结构体 G.S
	_, err = toml.DecodeFile(confPath, &G.S)
	if err != nil {
		return err
	}
	return nil
}
