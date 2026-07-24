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

	"golang.org/x/term"
	"gopkg.in/ini.v1"
)

// errInterrupted 表示交互式输入被用户中断（Ctrl+C）或 stdin 关闭（EOF）。
var errInterrupted = errors.New("cancelled")

func SetAliasConf(ctx context.Context, section string) error {
	if section == "" {
		return fmt.Errorf("alias name cannot be empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var conf Static
	var cfg *ini.File

	ensureConfPath()

	if _, err := os.Stat(ConfPath); err == nil {
		cfg, err = ini.Load(ConfPath)
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
				// 管道输入末尾无换行符时 ReadString 返回 (部分数据, io.EOF),
				// 已读到的数据应当接受, 仅无数据时才视为中断。
				if errors.Is(r.err, io.EOF) {
					if s := strings.TrimSpace(r.s); s != "" {
						return s, nil
					}
					myprint.Println("")
					return "", errInterrupted
				}
				return "", fmt.Errorf("read input: %w", r.err)
			}
			return strings.TrimSpace(r.s), nil
		}
	}

	// readSecret 读取密钥: 终端下不回显 (term.ReadPassword),
	// 非终端 (管道/重定向) 回退到普通行读取, 保持脚本可用。
	readSecret := func(prompt string) (string, error) {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return read(prompt)
		}
		myprint.Print(prompt)
		pw, err := term.ReadPassword(int(os.Stdin.Fd()))
		myprint.Println("")
		if err != nil {
			return "", fmt.Errorf("read secret: %w", err)
		}
		return strings.TrimSpace(string(pw)), nil
	}

	var err error
	for {
		conf.HostBase, err = read("Enter Host Base (e.g. https://s3.example.com): ")
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
		conf.SecretKey, err = readSecret("Enter Secret Key: ")
		if err != nil {
			return err
		}
		if conf.SecretKey == "" {
			myprint.PrintlnRed("Secret Key cannot be empty")
			continue
		}
		break
	}

	if conf.SessionToken, err = read("Enter Session Token (optional): "); err != nil {
		return err
	}

	if conf.Region, err = read("Enter Region (default 'us-east-1'): "); err != nil {
		return err
	}

	myprint.Printf("Bucket addressing style? ")
	if conf.BucketLookup, err = read("Mode: path / dns / https://www.%(bucket).example.com (default path): "); err != nil {
		return err
	}

	// 只能为 True / true / False / False 或者不输入
	for {
		input, err := read("Verify SSL certificate? (default True): ")
		if err != nil {
			return err
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
		input, err := read("Multipart Chunk Size (default 15): ")
		if err != nil {
			return err
		}
		if input != "" {
			m, err := strconv.Atoi(input)
			if err != nil || m <= 0 {
				myprint.PrintlnRed("Invalid input, please enter a positive number")
				continue
			}
			conf.MultipartChunkSizeMb = m
			break
		}
		conf.MultipartChunkSizeMb = 15
		break
	}

	// 交互不覆盖用户手工维护的非交互字段 (max_retries):
	// 重新 alias set 前从已有 section 读出旧值, 避免 ReflectFrom 后被 DeleteKey 擦掉。
	if cfg.HasSection(section) {
		old := cfg.Section(section)
		if old.HasKey("max_retries") {
			conf.MaxRetries, _ = old.Key("max_retries").Int()
		}
	}

	sec, err := cfg.NewSection(section)
	if err != nil {
		return fmt.Errorf("create section: %w", err)
	}
	if err := sec.ReflectFrom(&conf); err != nil {
		return fmt.Errorf("reflect config: %w", err)
	}

	if conf.SessionToken == "" {
		sec.DeleteKey("session_token")
	}
	if conf.VerifySSL {
		sec.DeleteKey("verify_ssl")
	}
	if conf.DefaultMimeType == "" {
		sec.DeleteKey("default_mime_type")
	}
	if conf.MultipartChunkSizeMb == 15 {
		sec.DeleteKey("multipart_chunk_size_mb")
	}
	if conf.Region == "" {
		sec.DeleteKey("region")
	}
	if conf.BucketLookup == "" {
		sec.DeleteKey("bucket_lookup")
	}
	if conf.MaxRetries == 0 {
		sec.DeleteKey("max_retries")
	}

	if err := saveConfig(cfg, ConfPath); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	myprint.PrintfGreen("S3 configuration saved to %s\n", ConfPath)
	return nil
}
