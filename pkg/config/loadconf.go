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

	mu.RLock()
	needInit := G.S == nil
	mu.RUnlock()
	if needInit {
		mu.Lock()
		if G.S == nil {
			G.S = make(map[string]Static)
		}
		mu.Unlock()
	}

	info, err := os.Stat(ConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("config file not found: %s", ConfigPath)
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
		newS[name] = s
	}
	SetSections(newS)
	return nil
}
