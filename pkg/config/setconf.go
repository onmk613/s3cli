package config

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	myprint "s3cli/pkg/fmtutil"

	"gopkg.in/ini.v1"
)

// errInterrupted 表示交互式输入被用户中断（Ctrl+C）或 stdin 关闭（EOF）。
var errInterrupted = errors.New("input cancelled")

func SetAliasConf(ctx context.Context, section string) error {
	if section == "" {
		return fmt.Errorf("alias name cannot be empty")
	}
	if ctx == nil {
		ctx = context.Background()
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
	// read 阻塞读取一行，同时监听 ctx 取消（Ctrl+C）。
	// 返回 errInterrupted 时调用方应立即终止，避免死循环。
	read := func(prompt string) (string, error) {
		myprint.Print(prompt)
		type result struct {
			s   string
			err error
		}
		ch := make(chan result, 1)
		go func() {
			s, err := reader.ReadString('\n')
			ch <- result{s, err}
		}()
		select {
		case <-ctx.Done():
			myprint.Println("")
			return "", errInterrupted
		case r := <-ch:
			if r.err != nil {
				if errors.Is(r.err, io.EOF) {
					myprint.Println("")
					return "", errInterrupted
				}
				return "", fmt.Errorf("read input: %w", r.err)
			}
			return strings.TrimSpace(r.s), nil
		}
	}

	var err error
	for {
		conf.HostBase, err = read("Enter Host Base (e.g. http://s3.example.com): ")
		if err != nil {
			return err
		}
		if conf.HostBase == "" {
			myprint.PrintlnRed("Host Base cannot be empty")
			continue
		}
		break
	}

	for {
		conf.AccessKey, err = read("Enter Access Key: ")
		if err != nil {
			return err
		}
		if conf.AccessKey == "" {
			myprint.PrintlnRed("Access Key cannot be empty")
			continue
		}
		break
	}

	for {
		conf.SecretKey, err = read("Enter Secret Key: ")
		if err != nil {
			return err
		}
		if conf.SecretKey == "" {
			myprint.PrintlnRed("Secret Key cannot be empty")
			continue
		}
		break
	}

	if conf.Region, err = read("Enter Region (default 'us-east-1'): "); err != nil {
		return err
	}

	myprint.Println("Bucket addressing style? ")
	if conf.BucketLookup, err = read("Mode: path / dns / https://www.%(bucket).example.com (default path): "); err != nil {
		return err
	}

	// 只能为 True / true / False / False 或者不输入
	for {
		input, rerr := read("Verify SSL certificate? (default True): ")
		if rerr != nil {
			return rerr
		}
		switch strings.ToLower(input) {
		case "true", "":
			conf.VerifySSL = true
		case "false":
			conf.VerifySSL = false
		default:
			myprint.PrintlnRed("Invalid input, please enter true/True or false/False")
			continue
		}
		break
	}

	if conf.DefaultMimeType, err = read("Default MimeType (default binary/octet-stream): "); err != nil {
		return err
	}

	for {
		input, rerr := read("Multipart Chunk Size (default 15): ")
		if rerr != nil {
			return rerr
		}
		if input != "" {
			m, err := strconv.Atoi(input)
			if err != nil {
				myprint.PrintlnRed("Invalid input, please enter a number")
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
	myprint.PrintfGreen("S3 configuration saved to %s\n", ConfigPath)
	return nil
}
