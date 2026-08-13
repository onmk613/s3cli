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
	"strconv"
	"strings"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/i18n"
	"s3cli/pkg/s3iface"
)

// GetOptions get 命令参数
type GetOptions struct {
	Recursive   bool
	Concurrency int
	Range       string // HTTP Range header (e.g. "bytes=0-1023"); 仅对单文件有效
	NoProgress  bool   // 不显示进度条（--quiet）
	Overwrite   bool   // 本地文件已存在时是否覆盖 (默认跳过)
	VersionID   string // --version-id/--vid: 下载指定版本
	Offset      int64  // -o: stdout 输出的起始偏移 (与 `get -` 配合)
	Tail        int64  // -t: 仅输出末尾 N 字节
	Lines       int    // -n: 仅输出前 N 行 (head)
}

// GetObject 下载对象
func (c *Action) GetObject(opt GetOptions, bucket, prefix, localPath string) error {
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
			return fmt.Errorf("check s3 path: %s", FormatAPIError(err))
		}
		if !ok {
			return errors.New(i18n.T("source is not a single object", "源不是单个对象"))
		}
		return c.downloadSingleFile(opt, bucket, prefix, localPath)
	}
	ok, err := c.IsS3File(bucket, prefix)
	if err != nil {
		return fmt.Errorf("check s3 path: %s", FormatAPIError(err))
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
	gopts := &s3iface.GetObjectOptions{VersionID: opt.VersionID}
	if rng := buildRangeFromGet(opt); rng != "" {
		gopts.Range = rng
	}
	out, err := c.S3.GetObject(c.Ctx, bucket, key, gopts)
	if err != nil {
		return fmt.Errorf("get %s: %s", c.S3Path(bucket, key), FormatAPIError(err))
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
		if strings.HasSuffix(line, "\r") {
			line = strings.TrimSuffix(line, "\r")
		}
		fmt.Fprintln(os.Stdout, line)
		count++
		if count >= n {
			break
		}
	}
	return sc.Err()
}

func (c *Action) downloadDirectory(opt GetOptions, bucket, key, localPath string) error {
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
			return c.forEachObject(ctx, bucket, key, func(obj s3iface.ObjectInfo) error {
				objKey := obj.Key
				if strings.HasSuffix(objKey, "/") && obj.Size == 0 {
					return nil
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
			// 默认不覆盖: 本地文件已存在则跳过 (静默, 进度条计入已完成)。
			if !opt.Overwrite {
				if info, statErr := os.Stat(job.Dst); statErr == nil && !info.IsDir() {
					return nil
				}
			}
			_, err := c.downloadFile(job.Src, job.Dst, bucket, report, "")
			return err
		},
	})
}

func (c *Action) downloadSingleFile(opt GetOptions, bucket, key, localPath string) error {
	localFilePath, err := determineLocalFilePath(localPath, key)
	if err != nil {
		return err
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
	size, err := c.downloadFile(key, localFilePath, bucket, nil, opt.VersionID)
	if err != nil {
		myprint.PrintlnRed(i18n.T("FAILED", "失败"))
		return fmt.Errorf("download: %s", FormatAPIError(err))
	}
	myprint.Printf("(%s)\n", FormatBytes(size))
	return nil
}

func (c *Action) rangeGetObject(bucket, key, localFilePath, rng, versionID string) error {
	if err := os.MkdirAll(filepath.Dir(localFilePath), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	out, err := c.S3.GetObject(c.Ctx, bucket, key, &s3iface.GetObjectOptions{
		Range:     rng,
		VersionID: versionID,
	})
	if err != nil {
		return fmt.Errorf("range get: %s", FormatAPIError(err))
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
		c.S3Path(bucket, key), rng, localFilePath, FormatBytes(written))
	return nil
}

func (c *Action) downloadFile(key, localPath, bucket string, report func(n int64), versionID string) (int64, error) {
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

	out, err := c.S3.GetObject(c.Ctx, bucket, key, &s3iface.GetObjectOptions{VersionID: versionID})
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
	if err := os.Rename(tmpPath, localPath); err != nil {
		return 0, fmt.Errorf("replace file: %w", err)
	}
	return n, nil
}

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
	if strings.HasPrefix(s3Key, s3Prefix) {
		relativePath = strings.TrimPrefix(strings.TrimPrefix(s3Key, s3Prefix), "/")
	}
	if relativePath == "" {
		return localBasePath, nil
	}
	return safeJoinLocal(localBasePath, relativePath)
}

// safeJoinLocal 把 S3 相对 key 拼接到本地目录下。
// S3 key 可包含 ".." 段, filepath.Join 清理后会逃出 base 目录 (路径穿越),
// 必须显式拒绝: bucket 中的恶意/异常 key 不应写出目标目录之外。
func safeJoinLocal(base, relSlash string) (string, error) {
	for _, seg := range strings.Split(relSlash, "/") {
		if seg == ".." {
			return "", fmt.Errorf(i18n.T("refusing to write outside %s: object key %q contains '..'", "拒绝写入 %s 之外：对象 key %q 包含 '..'"), base, relSlash)
		}
	}
	return filepath.Join(base, relSlash), nil
}
