package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	myprint "s3cli/pkg/fmtutil"

	"gopkg.in/ini.v1"
)

func SetAliasConf(section string) error {
	if section == "" {
		return fmt.Errorf("alias name cannot be empty")
	}

	var conf Static
	var cfg *ini.File

	if ConfigPath == "" {
		ConfigPath = DefaultConfigPath
	}

	if _, err := os.Stat(ConfigPath); err == nil {
		cfg, err = ini.Load(ConfigPath)
		if err != nil {
			return fmt.Errorf("load existing config: %w", err)
		}
	} else {
		cfg = ini.Empty()
	}

	reader := bufio.NewReader(os.Stdin)
	read := func(prompt string) string {
		myprint.Print(prompt)
		s, err := reader.ReadString('\n')
		if err != nil {
			return ""
		}
		return strings.TrimSpace(s)
	}

	for {
		conf.HostBase = read("Enter Host Base (e.g. http://s3.example.com): ")
		if conf.HostBase == "" {
			myprint.Errorln("Host Base cannot be empty")
			continue
		}
		break
	}

	for {
		conf.AccessKey = read("Enter Access Key: ")
		if conf.AccessKey == "" {
			myprint.Errorln("Access Key cannot be empty")
			continue
		}
		break
	}

	for {
		conf.SecretKey = read("Enter Secret Key: ")
		if conf.SecretKey == "" {
			myprint.Errorln("Secret Key cannot be empty")
			continue
		}
		break
	}

	conf.Region = read("Enter Region (default 'us-east-1'): ")

	myprint.Println("Bucket addressing style? ")
	conf.BucketLookup = read("Mode: path / dns / https://www.%(bucket).example.com (default path): ")

	// 只能为 True / true / False / False 或者不输入
	for {
		input := read("Verify SSL certificate? (default True): ")
		switch strings.ToLower(input) {
		case "true", "":
			conf.VerifySSL = true
		case "false":
			conf.VerifySSL = false
		default:
			myprint.Errorln("Invalid input, please enter true/True or false/False")
			continue
		}
		break
	}

	conf.DefaultMimeType = read("Default MimeType (default binary/octet-stream): ")

	for {
		input := read("Multipart Chunk Size (default 15): ")
		if input != "" {
			m, err := strconv.Atoi(input)
			if err != nil {
				myprint.Errorln("Invalid input, please enter a number")
				continue
			}
			conf.MultipartChunkSizeMb = m
			break
		}
		conf.MultipartChunkSizeMb = 15
		break
	}

	sec, err := cfg.NewSection(section)
	if err != nil {
		return fmt.Errorf("create section: %w", err)
	}
	if err := sec.ReflectFrom(&conf); err != nil {
		return fmt.Errorf("reflect config: %w", err)
	}
	if err := cfg.SaveTo(ConfigPath); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	myprint.Successln("S3 configuration saved to", ConfigPath)
	return nil
}
