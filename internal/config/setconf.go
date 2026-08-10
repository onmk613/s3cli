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
)

// errInterrupted 表示交互式输入被用户中断（Ctrl+C）或 stdin 关闭（EOF）。
var errInterrupted = errors.New("cancelled")

// lineResult 是交互输入通道的载荷。
type lineResult struct {
	s   string
	err error
}

// 终端能力钩子（测试注入用），避免直接依赖运行时终端状态。
var (
	isTerminal   = term.IsTerminal
	readPassword = term.ReadPassword
)

// SetAliasConf 写入/覆盖一个别名 (alias set)。
//   - 1 个参数 (ALIAS): 交互式填写, alias 不存在时从零开始, 已存在时显示旧值并可覆盖
//   - 4 个参数 (ALIAS URL ACCESSKEY SECRETKEY) 或 5 个 (含 SESSIONTOKEN): 非交互直接写入
func SetAliasConf(args []string) error {
	switch len(args) {
	case 1:
		return setAliasInteractive(strings.TrimSpace(args[0]))
	case 4, 5:
		sessionToken := ""
		if len(args) == 5 {
			sessionToken = args[4]
		}
		return setAliasStatic(strings.TrimSpace(args[0]), args[1], args[2], args[3], sessionToken)
	default:
		return fmt.Errorf("alias set accepts 1 arg (interactive) or 4/5 args (ALIAS URL ACCESSKEY SECRETKEY [SESSIONTOKEN]), got %d args", len(args))
	}
}

// setAliasInteractive 交互式创建/覆盖一个别名 (alias set <name>)。
// 配置文件不存在时自动新建；alias 已存在时从磁盘读入旧值供回车保留。
func setAliasInteractive(section string) error {
	if err := readConfig(G.C); err != nil {
		// 配置文件不存在时, 建新文件
		if !errors.Is(err, ErrConfigNotFoundOrEmpty) {
			return err
		}
		G.S = map[string]Static{}
	}

	conf, err := interactEdit(context.Background(), G.S[section])
	if err != nil {
		return err
	}
	return saveAlias(section, conf)
}

// EditAliasConf 交互式修改已有别名的配置 (alias edit)。
// alias 必须已存在；每个字段展示当前值，直接回车保留；必填字段
// (host_base / access_key / secret_key) 最终必须非空。
// 其余未交互字段 (default_mime_type / max_retries) 通过值拷贝自然保留。
func EditAliasConf(ctx context.Context, section string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	section = strings.TrimSpace(section)
	if section == "" {
		return errors.New("alias name cannot be empty")
	}

	if err := readConfig(G.C); err != nil {
		if !errors.Is(err, ErrConfigNotFoundOrEmpty) {
			return err
		}
		return fmt.Errorf("alias [%s] not found: config file %s does not exist, create it with `s3cli alias set %s URL ACCESSKEY SECRETKEY`", section, G.C, section)
	}
	old, ok := G.S[section]
	if !ok {
		return fmt.Errorf("alias [%s] not found in %s, create it with `s3cli alias set %s URL ACCESSKEY SECRETKEY`", section, G.C, section)
	}

	conf, err := interactEdit(ctx, old)
	if err != nil {
		return err
	}
	return saveAlias(section, conf)
}

// interactEdit 交互式填写/修改一个别名的字段。
// 每个字段展示当前值 (old)，空输入回车保留；必填字段最终必须非空。
// 终端下密钥不回显；非终端（管道/重定向）回退普通行读取。
func interactEdit(ctx context.Context, old Static) (Static, error) {
	conf := old

	reader := bufio.NewReader(os.Stdin)
	// 常驻单 goroutine 读 stdin，避免每次读取都新起一个阻塞 goroutine。
	// 注意：ReadString 对 stdin 的阻塞无法被 ctx 取消解除，只能随进程退出释放；
	// 发送侧用缓冲 channel + ctx 检查，避免 ctx 取消后发送永久阻塞。
	lines := make(chan lineResult, 1)
	go func() {
		defer close(lines)
		for {
			s, err := reader.ReadString('\n')
			if err != nil {
				// 纯 EOF（无数据）直接关闭通道, 由 read 的 !ok 分支统一处理;
				// 带数据的 EOF（管道末尾无换行）与其它错误仍发送, 保留数据与错误细节。
				if errors.Is(err, io.EOF) && strings.TrimSpace(s) == "" {
					return
				}
				select {
				case lines <- lineResult{s, err}:
				case <-ctx.Done():
					return
				}
				return
			}
			select {
			case lines <- lineResult{s, nil}:
			case <-ctx.Done():
				return
			}
		}
	}()

	// read 读取一行；空输入返回默认值 def（回车即保留旧值）。
	// ctx 取消或 stdin 无数据关闭时返回 errInterrupted，调用方应立即终止。
	read := func(prompt, def string) (string, error) {
		def = strings.TrimSpace(def)
		for {
			if def != "" {
				myprint.Printf("%s [%s]: ", prompt, def)
			} else {
				myprint.Printf("%s: ", prompt)
			}
			select {
			case <-ctx.Done():
				myprint.Println("")
				return "", errInterrupted
			case r, ok := <-lines:
				if !ok {
					myprint.Println("")
					return "", errInterrupted
				}
				s := strings.TrimSpace(r.s)
				if r.err != nil {
					// 纯 EOF（无数据）由 goroutine 直接关闭通道, 走上面的 !ok 分支;
					// 此处只处理带数据的 EOF（管道末尾无换行, 数据应接受）与其它错误。
					if errors.Is(r.err, io.EOF) && s != "" {
						return s, nil
					}
					return "", fmt.Errorf("read input: %w", r.err)
				}
				if s == "" && def != "" {
					return def, nil
				}
				return s, nil
			}
		}
	}

	// readSecret 读取密钥: 终端下不回显 (term.ReadPassword)，
	// 非终端 (管道/重定向) 回退到 read；空输入保留旧值。
	readSecret := func(prompt, def string) (string, error) {
		if !isTerminal(int(os.Stdin.Fd())) {
			return read(prompt, def)
		}
		myprint.Print(prompt)
		pw, err := readPassword(int(os.Stdin.Fd()))
		myprint.Println("")
		if errors.Is(err, io.EOF) {
			return "", errInterrupted
		}
		if err != nil {
			return "", fmt.Errorf("read secret: %w", err)
		}
		s := strings.TrimSpace(string(pw))
		if s == "" {
			return def, nil
		}
		return s, nil
	}

	var err error
	for {
		conf.HostBase, err = read("Host Base (e.g. https://s3.example.com)", conf.HostBase)
		if err != nil {
			return conf, err
		}
		if conf.HostBase == "" {
			myprint.PrintlnRed("Host Base cannot be empty")
			continue
		}
		break
	}

	for {
		conf.AccessKey, err = read("Access Key", conf.AccessKey)
		if err != nil {
			return conf, err
		}
		if conf.AccessKey == "" {
			myprint.PrintlnRed("Access Key cannot be empty")
			continue
		}
		break
	}

	for {
		conf.SecretKey, err = readSecret("Secret Key", conf.SecretKey)
		if err != nil {
			return conf, err
		}
		if conf.SecretKey == "" {
			myprint.PrintlnRed("Secret Key cannot be empty")
			continue
		}
		break
	}

	if conf.SessionToken, err = read("Session Token (optional, '-' to clear)", conf.SessionToken); err != nil {
		return conf, err
	}
	if conf.SessionToken == "-" {
		conf.SessionToken = "" // 允许清空已过期的 token
	}

	if conf.Region, err = read("Region", conf.Region); err != nil {
		return conf, err
	}

	for {
		if conf.BucketLookup, err = read("Bucket addressing style (path / dns / template with %(bucket))", conf.BucketLookup); err != nil {
			return conf, err
		}
		if conf.BucketLookup == "" {
			break // 空 = 默认 path
		}
		if _, _, verr := conf.ResolveBucketLookup(); verr == nil {
			break
		}
		myprint.PrintlnRed("Invalid style, expected path / dns / custom template containing %(bucket)")
	}

	for {
		input, err := read("No Verify SSL certificate (true/false)", strconv.FormatBool(conf.NoVerifySSL))
		if err != nil {
			return conf, err
		}
		b, perr := strconv.ParseBool(input)
		if perr == nil {
			conf.NoVerifySSL = b
			break
		}
		myprint.PrintlnRed("Invalid input, please enter true or false")
	}

	for {
		// 0 表示未设置（运行时取默认 DefaultPartSizeMB），回车保留当前值。
		input, err := read("Multipart Chunk Size MB (0 = default 15)", strconv.Itoa(conf.MultipartChunkSizeMb))
		if err != nil {
			return conf, err
		}
		m, aerr := strconv.Atoi(input)
		if aerr != nil || m < 0 {
			myprint.PrintlnRed("Invalid input, please enter a non-negative number")
			continue
		}
		conf.MultipartChunkSizeMb = m
		break
	}

	return conf, nil
}

// saveAlias 保存别名到全局表并原子写盘。
func saveAlias(section string, conf Static) error {
	G.S[section] = conf
	if err := saveConfig(G.C); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	myprint.PrintfGreen("S3 configuration saved to %s\n", G.C)
	return nil
}

// setAliasStatic 非交互写入单个别名的核心字段；其余字段通过值拷贝保留旧值。
func setAliasStatic(section, hostBase, accessKey, secretKey, sessionToken string) error {
	if err := readConfig(G.C); err != nil {
		// 配置文件不存在时, 建新文件
		if !errors.Is(err, ErrConfigNotFoundOrEmpty) {
			return err
		}
		G.S = map[string]Static{}
	}

	conf := G.S[section]
	conf.HostBase = hostBase
	conf.AccessKey = accessKey
	conf.SecretKey = secretKey
	conf.SessionToken = sessionToken
	return saveAlias(section, conf)
}
