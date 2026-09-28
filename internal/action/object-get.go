// object-get.go 实现对象下载 GetObject: 单文件直传与目录递归下载,
// 走 RunStream 并发框架, 默认跳过已存在本地文件 (--overwrite 强制覆盖),
// 含路径穿越防护与临时文件原子替换.

package action

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"s3cli/internal/api"
	myprint "s3cli/internal/fmtutil"
	"s3cli/internal/i18n"
)

// GetOptions get 命令参数
type GetOptions struct {
	Recursive   bool
	Concurrency int
	Range       string   // HTTP Range header (e.g. "bytes=0-1023"); 仅对单文件有效
	NoProgress  bool     // 不显示进度条（--quiet）
	Overwrite   bool     // 本地文件已存在时是否覆盖 (默认跳过)
	VersionID   string   // --version-id/--vid: 下载指定版本
	Offset      int64    // -o: stdout 输出的起始偏移 (与 `get -` 配合)
	Tail        int64    // -t: 仅输出末尾 N 字节
	Lines       int      // -n: 仅输出前 N 行 (head)
	Checksum    string   // --checksum: 下载后按该算法校验完整性 (CRC32/CRC32C/SHA1/SHA256)
	DryRun      bool     // --dry-run: 只列出将下载的对象, 不做实际下载
	Include     []string // --include: 只下载相对 key 匹配该 glob 的对象
	Exclude     []string // --exclude: 跳过相对 key 匹配该 glob 的对象
}

// GetObject 下载对象
func (c *Action) GetObject(opt GetOptions, bucket, prefix, localPath string) error {
	// 数值上界必须在 RunStream 分配 channel/goroutine 之前校验: 见 limits.go。
	if opt.Concurrency <= 0 {
		opt.Concurrency = defaultConcurrency
	}
	if err := validateConcurrency(opt.Concurrency); err != nil {
		return err
	}
	// stdout 模式 (get <alias:bucket/key> -): 流式输出对象内容, 替代旧 cat 命令.
	if localPath == "-" {
		return c.catToStdout(opt, bucket, prefix)
	}
	if opt.VersionID != "" {
		if opt.Recursive {
			return errors.New(i18n.T("--version-id cannot be used with -r/--recursive", "--version-id 不能与 -r/--recursive 一起使用"))
		}
		if opt.Range != "" {
			return errors.New(i18n.T("--version-id cannot be used with --range", "--version-id 不能与 --range 一起使用"))
		}
		ok, err := c.IsS3File(bucket, prefix)
		if err != nil {
			return fmt.Errorf("check s3 path: %w", err)
		}
		if !ok {
			return errors.New(i18n.T("source is not a single object", "源不是单个对象"))
		}
		return c.downloadSingleFile(opt, bucket, prefix, localPath)
	}
	ok, err := c.IsS3File(bucket, prefix)
	if err != nil {
		return fmt.Errorf("check s3 path: %w", err)
	}
	if !ok || prefix == "" {
		if !opt.Recursive {
			return errors.New(i18n.T("source is a directory; use -r/--recursive", "源是目录；请使用 -r/--recursive"))
		}
		if opt.Range != "" {
			return errors.New(i18n.T("--range cannot be used with --recursive", "--range 不能与 --recursive 一起使用"))
		}
		return c.downloadDirectory(opt, bucket, prefix, localPath)
	}
	return c.downloadSingleFile(opt, bucket, prefix, localPath)
}

// catToStdout 把对象内容流式写到 stdout (get -), 替代旧 cat 命令.
func (c *Action) catToStdout(opt GetOptions, bucket, key string) error {
	if key == "" {
		return errors.New(i18n.T("stdout output requires a single object key", "stdout 输出需要指定单个对象 key"))
	}
	gopts := &api.GetObjectOptions{VersionID: opt.VersionID}
	if rng := buildRangeFromGet(opt); rng != "" {
		gopts.Range = rng
	}
	out, err := c.S3.GetObject(c.Ctx, bucket, key, gopts)
	if err != nil {
		return fmt.Errorf("get %s: %w", c.S3Path(bucket, key), err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(out.Body)
	if opt.Lines > 0 {
		return writeHeadLines(out.Body, opt.Lines)
	}
	if _, err := io.Copy(os.Stdout, out.Body); err != nil {
		return fmt.Errorf("write stdout: %w", err)
	}
	return nil
}

// buildRangeFromGet 由 --range/--offset/--tail 计算 Range 头.
func buildRangeFromGet(opt GetOptions) string {
	if opt.Range != "" {
		return opt.Range
	}
	if opt.Tail > 0 {
		return "bytes=-" + strconv.FormatInt(opt.Tail, 10)
	}
	if opt.Offset > 0 {
		return "bytes=" + strconv.FormatInt(opt.Offset, 10) + "-"
	}
	return ""
}

// writeHeadLines 只输出前 n 行 (head -n).
func writeHeadLines(r io.Reader, n int) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var count int
	for sc.Scan() {
		line := sc.Text()
		line = strings.TrimSuffix(line, "\r")
		fmt.Fprintln(os.Stdout, line)
		count++
		if count >= n {
			break
		}
	}
	return sc.Err()
}

func (c *Action) downloadDirectory(opt GetOptions, bucket, key, localPath string) error {
	alg, err := parseChecksumAlg(opt.Checksum)
	if err != nil {
		return err
	}
	// 目录源的列举前缀必须规范化为 "dir/": 裸前缀会连带列出兄弟目录
	// ("logs-2023/x"), 而下游按字面量剥离 key 推导相对路径, 会写出
	// "-2023/x" 这种本地垃圾路径。mirror / rm / cp / mv 同一口径。
	// (filepath.Base 会忽略尾部 "/", 因此 determineLocalBasePath 的结果不变。)
	key = normalizeDirPrefix(key)
	localBasePath, err := determineLocalBasePath(localPath, bucket, key)
	if err != nil {
		return err
	}

	return RunStream(c.Ctx, StreamConfig{
		Concurrency: opt.Concurrency,
		Label:       "get",
		NoProgress:  opt.NoProgress,
		Count: func(ctx context.Context, add func(n, size int64)) error {
			return c.countS3Prefix(ctx, bucket, key, true, add)
		},
		Scan: func(ctx context.Context, jobs chan<- StreamJob) error {
			return c.forEachObject(ctx, bucket, key, func(obj api.ObjectInfo) error {
				objKey := obj.Key
				if strings.HasSuffix(objKey, "/") && obj.Size == 0 {
					return nil
				}
				if len(opt.Include) > 0 || len(opt.Exclude) > 0 {
					// 与 mirror / rm / put 同一套过滤语义: 相对参数前缀匹配。
					if !matchesMirrorFilters(relKeyForDelete(objKey, key), opt.Include, opt.Exclude) {
						return nil
					}
				}
				localFilePath, pathErr := buildLocalFilePath(objKey, key, localBasePath)
				if pathErr != nil {
					// 单个异常 key (如含 ".." 的路径穿越) 警告并跳过,
					// 不中断整个目录下载。
					myprint.PrintfYellow(i18n.T("skip %s: %v\n", "跳过 %s：%v\n"), objKey, pathErr)
					return nil
				}
				jobs <- StreamJob{
					Src:  objKey,
					Dst:  localFilePath,
					Size: obj.Size,
				}
				return nil
			})
		},
		Work: func(ctx context.Context, job StreamJob, report func(n int64)) error {
			if opt.DryRun {
				myprint.PrintfYellow(i18n.T("would download %s -> %s (%s)\n", "将下载 %s -> %s（%s）\n"),
					c.S3Path(bucket, job.Src), job.Dst, myprint.FormatBytes(job.Size))
				return nil
			}
			// 默认不覆盖: 本地文件已存在则跳过 (静默, 进度条计入已完成)。
			if !opt.Overwrite {
				if info, statErr := os.Stat(job.Dst); statErr == nil && !info.IsDir() {
					return nil
				}
			}
			_, err := c.downloadFile(job.Src, job.Dst, bucket, report, "", alg)
			return err
		},
	})
}

func (c *Action) downloadSingleFile(opt GetOptions, bucket, key, localPath string) error {
	localFilePath, err := determineLocalFilePath(localPath, key)
	if err != nil {
		return err
	}

	alg, err := parseChecksumAlg(opt.Checksum)
	if err != nil {
		return err
	}
	if alg != "" && opt.Range != "" {
		return errors.New(i18n.T("--checksum cannot be used with --range (a partial object has no whole-object checksum)", "--checksum 不能与 --range 一起使用（部分对象没有整体校验和）"))
	}

	if opt.DryRun {
		size, sizeErr := c.headObjectSize(bucket, key, opt.VersionID)
		if sizeErr != nil {
			return sizeErr
		}
		myprint.PrintfYellow(i18n.T("would download %s -> %s (%s)\n", "将下载 %s -> %s（%s）\n"),
			c.S3Path(bucket, key), localFilePath, myprint.FormatBytes(size))
		return nil
	}

	// --range 直接走 GetObject (显式字节范围, 始终覆盖)
	if opt.Range != "" {
		return c.rangeGetObject(bucket, key, localFilePath, opt.Range, opt.VersionID)
	}

	// 默认不覆盖: 本地文件已存在则跳过, 仅 --overwrite 时强制下载。
	if !opt.Overwrite {
		if info, statErr := os.Stat(localFilePath); statErr == nil && !info.IsDir() {
			myprint.Printf(i18n.T("skip: %s already exists\n", "跳过：%s 已存在\n"), localFilePath)
			return nil
		}
	}

	myprint.Printf(i18n.T("get: %s --> %s ", "下载：%s --> %s "), c.S3Path(bucket, key), localFilePath)
	size, err := c.downloadFile(key, localFilePath, bucket, nil, opt.VersionID, alg)
	if err != nil {
		myprint.PrintlnRed(i18n.T("FAILED", "失败"))
		return fmt.Errorf("download: %w", err)
	}
	myprint.Printf("(%s)\n", myprint.FormatBytes(size))
	return nil
}

func (c *Action) rangeGetObject(bucket, key, localFilePath, rng, versionID string) error {
	if err := os.MkdirAll(filepath.Dir(localFilePath), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	out, err := c.S3.GetObject(c.Ctx, bucket, key, &api.GetObjectOptions{
		Range:     rng,
		VersionID: versionID,
	})
	if err != nil {
		return fmt.Errorf("range get: %w", err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(out.Body)

	file, err := os.CreateTemp(filepath.Dir(localFilePath), ".s3cli-download-*")
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	tmpPath := file.Name()
	defer os.Remove(tmpPath)
	defer func(file *os.File) {
		_ = file.Close()
	}(file)

	written, err := file.ReadFrom(out.Body)
	if err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close file: %w", err)
	}
	if err := os.Rename(tmpPath, localFilePath); err != nil {
		return fmt.Errorf("replace file: %w", err)
	}
	myprint.Printf(i18n.T("get: %s [%s] --> %s (%s)\n", "下载：%s [%s] --> %s（%s）\n"),
		c.S3Path(bucket, key), rng, localFilePath, myprint.FormatBytes(written))
	return nil
}

// downloadFile 下载单个对象到 localPath。
//
// checksumAlg 非空时: 请求服务端返回附加校验和 (x-amz-checksum-mode: ENABLED),
// 下载完成后在临时文件上重算并比对, 不一致则中止 (不 rename, 不留半个文件)。
// 服务端未返回该校验和时明确报错, 而不是静默当作通过。
func (c *Action) downloadFile(key, localPath, bucket string, report func(n int64), versionID string, checksumAlg api.ChecksumAlgorithm) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return 0, fmt.Errorf("mkdir: %w", err)
	}
	file, err := os.CreateTemp(filepath.Dir(localPath), ".s3cli-download-*")
	if err != nil {
		return 0, fmt.Errorf("create file: %w", err)
	}
	tmpPath := file.Name()
	defer os.Remove(tmpPath)
	defer func(file *os.File) {
		_ = file.Close()
	}(file)

	getOpts := &api.GetObjectOptions{VersionID: versionID}
	if checksumAlg != "" {
		getOpts.ChecksumMode = "ENABLED"
	}
	out, err := c.S3.GetObject(c.Ctx, bucket, key, getOpts)
	if err != nil {
		return 0, err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(out.Body)

	// 有进度回调时，用计数 reader 包装 body 流。
	var body io.Reader = out.Body
	if report != nil {
		body = &countingReader{r: out.Body, report: report}
	}

	n, err := io.Copy(file, body)
	if err != nil {
		return 0, fmt.Errorf("write file: %w", err)
	}
	if err := file.Close(); err != nil {
		return 0, fmt.Errorf("close file: %w", err)
	}
	if checksumAlg != "" {
		if err := verifyDownloadedChecksum(tmpPath, out.Checksum, checksumAlg, c.S3Path(bucket, key)); err != nil {
			return 0, err
		}
	}
	// os.CreateTemp 的 0600 权限对下载产物过严 (脚本/配置下载后不可执行、不可共读),
	// 按 umask 放宽到常规新建文件权限 (通常 0644)。
	chmodDownloaded(tmpPath)
	// 保留服务端 LastModified: 否则 put 后立刻 diff --quick 会因时钟不同必报差异。
	if !out.LastModified.IsZero() {
		_ = os.Chtimes(tmpPath, out.LastModified, out.LastModified)
	}
	if err := os.Rename(tmpPath, localPath); err != nil {
		return 0, fmt.Errorf("replace file: %w", err)
	}
	return n, nil
}

// headObjectSize 取对象大小 (--dry-run 需要在不下载的前提下报告体积)。
func (c *Action) headObjectSize(bucket, key, versionID string) (int64, error) {
	head, err := c.S3.HeadObject(c.Ctx, bucket, key, versionID)
	if err != nil {
		return 0, fmt.Errorf("head %s: %w", c.S3Path(bucket, key), err)
	}
	return head.ContentLength, nil
}

// chmodDownloaded 的平台实现见 umask_unix.go / umask_windows.go。

// countingReader 包装 io.Reader, 按读取进度实时上报字节增量.
type countingReader struct {
	r      io.Reader
	report func(n int64)
}

func (cr *countingReader) Read(p []byte) (int, error) {
	n, err := cr.r.Read(p)
	if n > 0 && cr.report != nil {
		cr.report(int64(n))
	}
	return n, err
}

// ---- 路径辅助 ----

func determineLocalBasePath(localPath, bucket, key string) (string, error) {
	if localPath != "" {
		info, err := os.Stat(localPath)
		if err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("stat path: %w", err)
		}
		if err == nil && !info.IsDir() {
			return "", fmt.Errorf(i18n.T("%s is not a directory", "%s 不是目录"), localPath)
		}
		return localPath, nil
	}
	if key != "" {
		return filepath.Base(key), nil
	}
	return bucket, nil
}

func determineLocalFilePath(localPath, key string) (string, error) {
	if localPath == "" {
		return filepath.Base(key), nil
	}
	info, err := os.Stat(localPath)
	if err == nil {
		if info.IsDir() {
			return filepath.Join(localPath, filepath.Base(key)), nil
		}
		return localPath, nil
	}
	if os.IsNotExist(err) {
		parent := filepath.Dir(localPath)
		if parent != "." && parent != "/" {
			if fileInfo, err := os.Stat(parent); err != nil || !fileInfo.IsDir() {
				return "", fmt.Errorf(i18n.T("parent directory %s does not exist", "父目录 %s 不存在"), parent)
			}
		}
		return localPath, nil
	}
	return "", fmt.Errorf("stat path: %w", err)
}

func buildLocalFilePath(s3Key, s3Prefix, localBasePath string) (string, error) {
	s3Key = strings.TrimPrefix(s3Key, "/")
	s3Prefix = strings.TrimPrefix(s3Prefix, "/")
	if s3Prefix == "" {
		return safeJoinLocal(localBasePath, s3Key)
	}
	// 去掉前缀及其后可选的斜杠, 等价于原正则 "^s3Prefix/?", 但避免下载热路径重复编译正则.
	relativePath := s3Key
	if after, ok := strings.CutPrefix(s3Key, s3Prefix); ok {
		relativePath = strings.TrimPrefix(after, "/")
	}
	if relativePath == "" {
		return localBasePath, nil
	}
	return safeJoinLocal(localBasePath, relativePath)
}

// safeJoinLocal 把 S3 相对 key 拼接到本地目录下。
// S3 key 可包含 ".." 段, filepath.Join 清理后会逃出 base 目录 (路径穿越),
// 必须显式拒绝: bucket 中的恶意/异常 key 不应写出目标目录之外。
//
// 判段前先把反斜杠归一为 "/": Windows 的 filepath.Join 把 "..\..\evil"
// 当作分段路径, 只按 "/" 拆段会漏判 (单段 "..\..\evil" != ".." 但拼出的
// 路径同样逃出 base)。归一后逐段拒绝 "..", 与平台无关。
func safeJoinLocal(base, relSlash string) (string, error) {
	normalized := strings.ReplaceAll(relSlash, "\\", "/")
	if slices.Contains(strings.Split(normalized, "/"), "..") {
		return "", fmt.Errorf(i18n.T("refusing to write outside %s: object key %q contains '..'", "拒绝写入 %s 之外：对象 key %q 包含 '..'"), base, relSlash)
	}
	return filepath.Join(base, relSlash), nil
}

// verifyDownloadedChecksum 按算法重算已落盘文件的校验和并与服务端返回值比对。
//
// 单独读一遍临时文件 (而不是包一层 TeeReader): 下载路径已经有一次 SHA256
// 预计算, 再加一个 tee 会让内存与分支复杂度上升, 而校验和校验是显式 opt-in
// 的动作, 多一次顺序读是可接受的代价。
func verifyDownloadedChecksum(path string, server api.ChecksumValues, alg api.ChecksumAlgorithm, display string) error {
	want := server.Get(alg)
	if want == "" {
		return fmt.Errorf(i18n.T(
			"checksum verification requested for %s but the server returned no %s checksum (object may predate additional checksums, or the endpoint does not support x-amz-checksum-mode)",
			"已请求校验 %s，但服务端未返回 %s 校验和（对象可能是更早写入的，或该端点不支持 x-amz-checksum-mode）"),
			display, alg)
	}

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open downloaded file for checksum: %w", err)
	}
	defer func(f *os.File) { _ = f.Close() }(f)

	got, err := api.ComputeChecksumReader(alg, f)
	if err != nil {
		return fmt.Errorf("compute checksum: %w", err)
	}
	if got != want {
		return fmt.Errorf(i18n.T(
			"checksum mismatch for %s: server %s=%s, downloaded %s=%s",
			"%s 的校验和不匹配：服务端 %s=%s，下载得到 %s=%s"),
			display, alg, want, alg, got)
	}
	return nil
}
