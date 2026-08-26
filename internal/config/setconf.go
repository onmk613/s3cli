package config

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"

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

type inputReq struct {
	secret bool
	resp   chan lineResult // cap 1，owner 发送不会阻塞
}

func stdinOwner(ctx context.Context, reqs <-chan inputReq) {
	reader := bufio.NewReader(os.Stdin)
	for {
		select {
		case <-ctx.Done():
			return
		case req, ok := <-reqs:
			if !ok {
				return
			}
			var res lineResult
			if req.secret {
				res.s, res.err = readSecretLine(reader)
			} else {
				res.s, res.err = readPlainLine(reader)
			}
			req.resp <- res
		}
	}
}

func readPlainLine(r *bufio.Reader) (string, error) {
	s, err := r.ReadString('\n')
	s = strings.TrimRight(s, "\r\n") // 统一吃掉 CRLF
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return s, err // 带数据的 EOF 交给调用方判定
}

func readSecretLine(r *bufio.Reader) (string, error) {
	fd := int(os.Stdin.Fd())
	if !isTerminal(fd) {
		return readPlainLine(r) // 管道/重定向
	}
	old, err := term.MakeRaw(fd)
	if err != nil {
		return readPlainLine(r)
	}
	var once sync.Once
	restore := func() { once.Do(func() { _ = term.Restore(fd, old) }) }
	defer restore()

	// raw 模式下 ISIG 被关闭，Ctrl+C 以字节 3 到达；但仍要防 SIGTERM/SIGHUP
	// 打断导致终端残留 raw+无回显（补救命令：stty sane）
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sig)
	go func() {
		if _, ok := <-sig; ok {
			restore()
			myprint.Print("\r\n")
			os.Exit(143)
		}
	}()

	var buf []rune
	for {
		c, _, err := r.ReadRune()
		if err != nil {
			myprint.Print("\r\n")
			if errors.Is(err, io.EOF) && len(buf) > 0 {
				return string(buf), nil
			}
			return "", err
		}
		switch {
		case c == '\r' || c == '\n':
			// 粘贴常见 CRLF：把配对的另一半吃掉，别留给下一个 prompt
			if r.Buffered() > 0 {
				if p, _ := r.Peek(1); len(p) == 1 &&
					(p[0] == '\r' || p[0] == '\n') && rune(p[0]) != c {
					_, _ = r.Discard(1)
				}
			}
			myprint.Print("\r\n") // raw 模式必须 \r\n，否则光标不回行首→阶梯错位
			return string(buf), nil

		case c == 3: // Ctrl+C
			myprint.Print("\r\n")
			return "", errInterrupted
		case c == 4: // Ctrl+D
			if len(buf) == 0 {
				myprint.Print("\r\n")
				return "", io.EOF
			}
		case c == 127 || c == 8: // Backspace
			if len(buf) > 0 {
				buf = buf[:len(buf)-1]
			}
		case c == 21: // Ctrl+U
			buf = buf[:0]
		case c == 27: // 吞掉方向键 / bracketed-paste 的 ESC[200~、ESC[201~
			if r.Buffered() > 0 {
				skipEscape(r)
			}
		case c == '\t' || c >= 32:
			buf = append(buf, c)
		}
	}
}

func skipEscape(r *bufio.Reader) {
	b, err := r.Peek(1)
	if err != nil || len(b) == 0 || (b[0] != '[' && b[0] != 'O') {
		return
	}
	_, _ = r.Discard(1)
	for {
		c, err := r.ReadByte()
		if err != nil || (c >= 0x40 && c <= 0x7e) { // CSI 终止符，含 '~' 'A'
			return
		}
	}
}

func resp2req(secret bool, resp chan lineResult) inputReq {
	return inputReq{secret: secret, resp: resp}
}

// 终端能力钩子（测试注入用），避免直接依赖运行时终端状态。
var (
	isTerminal   = term.IsTerminal
	readPassword = term.ReadPassword
)

// SetAliasConf 写入/覆盖一个别名 (alias set)。
//   - 1 个参数 (ALIAS): 交互式填写, alias 不存在时从零开始, 已存在时显示旧值并可覆盖
//   - 4 个参数 (ALIAS URL ACCESSKEY SECRETKEY) 或 5 个 (含 SESSIONTOKEN): 非交互直接写入
func SetAliasConf(ctx context.Context, args []string) error {
	switch len(args) {
	case 1:
		return setAliasInteractive(ctx, strings.TrimSpace(args[0]))
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
func setAliasInteractive(ctx context.Context, section string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := readConfig(G.C); err != nil {
		// 配置文件不存在时, 建新文件
		if !errors.Is(err, ErrConfigNotFoundOrEmpty) {
			return err
		}
		G.S = map[string]Static{}
	}

	conf, err := interactEdit(ctx, G.S[section])
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
	reqs := make(chan inputReq)
	go stdinOwner(ctx, reqs)

	ask := func(prompt, def string, secret bool) (string, error) {
		def = strings.TrimSpace(def)
		switch {
		case secret && def != "":
			myprint.Printf("%s [keep current]: ", prompt) // 绝不回显旧密钥
		case def != "":
			myprint.Printf("%s [%s]: ", prompt, def)
		default:
			myprint.Printf("%s: ", prompt)
		}

		resp := make(chan lineResult, 1)
		select {
		case <-ctx.Done():
			myprint.Println("")
			return "", errInterrupted
		case reqs <- resp2req(secret, resp):
		}

		select {
		case <-ctx.Done():
			myprint.Println("")
			return "", errInterrupted
		case res := <-resp:
			s := res.s
			if !secret {
				s = strings.TrimSpace(s) // 密钥只裁 CRLF，首尾空格可能是合法字符
			}
			switch {
			case res.err == nil:
			case errors.Is(res.err, errInterrupted):
				return "", errInterrupted
			case errors.Is(res.err, io.EOF):
				if s == "" { // 纯 EOF
					myprint.Println("")
					return "", errInterrupted
				} // 带数据的 EOF（管道末尾无换行）→ 接受
			default:
				return "", fmt.Errorf("read input: %w", res.err)
			}
			if s == "" && def != "" {
				return def, nil
			}
			return s, nil
		}
	}

	read := func(p, d string) (string, error) { return ask(p, d, false) }
	readSecret := func(p, d string) (string, error) { return ask(p, d, true) }

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
// 字段值统一 TrimSpace, 并对必填字段做非空校验 —— 此前空凭证会落盘,
// 到使用时才由 api.New 报出误导性错误。
func setAliasStatic(section, hostBase, accessKey, secretKey, sessionToken string) error {
	section = strings.TrimSpace(section)
	hostBase = strings.TrimSpace(hostBase)
	accessKey = strings.TrimSpace(accessKey)
	secretKey = strings.TrimSpace(secretKey)
	sessionToken = strings.TrimSpace(sessionToken)

	switch {
	case section == "":
		return errors.New("alias name cannot be empty")
	case hostBase == "":
		return errors.New("host base (URL) cannot be empty")
	case accessKey == "":
		return errors.New("access key cannot be empty")
	case secretKey == "":
		return errors.New("secret key cannot be empty")
	}

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
