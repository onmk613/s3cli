package action

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/utils"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type DiffEndpoint struct {
	IsS3          bool
	S3            *s3.Client
	Ctx           context.Context
	Alias         string
	Bucket        string
	Key           string
	TrailingSlash bool
	Path          string
}

func (e *DiffEndpoint) String() string {
	if e.IsS3 {
		return S3PathStatic(e.Alias, e.Bucket, e.Key)
	}
	return e.Path
}

type DiffMode string

const (
	// DiffModeSize 仅比较大小。最快，但可能漏掉同大小的不同内容。
	DiffModeSize DiffMode = "size"
	// DiffModeQuick 大小+(本地 mtime / s3 last-modified)。
	DiffModeQuick DiffMode = "quick"
	// DiffModeMD5 大小相同时，流式读取双方算 MD5 比较（默认）。
	DiffModeMD5 DiffMode = "md5"
)

// DiffOptions diff 主入口参数。
type DiffOptions struct {
	A *DiffEndpoint
	B *DiffEndpoint

	Mode        DiffMode
	Recursive   bool // 目录模式是否递归（默认 true）
	Concurrency int  // 比对内容时的并发（仅目录模式生效）
	BriefOnly   bool // 只打印差异，不打印 "Identical" 列表
}

type fileEntry struct {
	Path string // 在该端的“相对路径”（目录模式下相对于目录根）
	Size int64
	// 可选：本地 mtime 或 s3 last-modified 的 Unix 秒；目前仅 quick 模式使用
	Mtime int64
	// 可选：ETag（去引号），仅 s3 端有
	ETag string
}

// =============== 入口判定：local vs s3 ===============

// ParseDiffArg 解析 diff 的一个参数。规则：
//
//   - 形如 "alias:bucket[/key]" 且 alias 出现在配置里 -> S3 端点
//   - 否则视为本地路径（不要求文件存在；后续会单独检查）
//
// 调用方需提供一个判断 alias 是否存在的回调（保持 action 包不依赖 config）。
func ParseDiffArg(ctx context.Context, arg string, aliasExists func(string) bool, makeClient func(*utils.S3Path) (*s3.Client, error)) (*DiffEndpoint, error) {
	// 先尝试 ParseS3Path
	if colon := strings.Index(arg, ":"); colon > 0 {
		alias := arg[:colon]
		if aliasExists(alias) {
			sp, err := utils.ParseS3Path(arg)
			if err != nil {
				return nil, err
			}
			if sp.Bucket == "" {
				return nil, fmt.Errorf("diff: s3 path %q must contain a bucket", arg)
			}
			cli, err := makeClient(sp)
			if err != nil {
				return nil, err
			}
			return &DiffEndpoint{
				IsS3:          true,
				S3:            cli,
				Ctx:           ctx,
				Alias:         sp.Alias,
				Bucket:        sp.Bucket,
				Key:           sp.Key,
				TrailingSlash: sp.TrailingSlash,
			}, nil
		}
	}

	// 本地路径
	abs, err := filepath.Abs(arg)
	if err != nil {
		return nil, fmt.Errorf("resolve local path %q: %w", arg, err)
	}
	return &DiffEndpoint{IsS3: false, Path: abs}, nil
}

// Diff 比较两端。自动识别“文件 vs 目录”。
func Diff(opt DiffOptions) error {
	if opt.A == nil || opt.B == nil {
		return fmt.Errorf("diff: both A and B endpoints are required")
	}
	if opt.Mode == "" {
		opt.Mode = DiffModeMD5
	}
	if opt.Concurrency <= 0 {
		opt.Concurrency = 8
	}

	aIsDir, aErr := endpointIsDir(opt.A)
	if aErr != nil {
		return fmt.Errorf("inspect A: %w", aErr)
	}
	bIsDir, bErr := endpointIsDir(opt.B)
	if bErr != nil {
		return fmt.Errorf("inspect B: %w", bErr)
	}

	if aIsDir != bIsDir {
		return fmt.Errorf("diff: cannot compare a file against a directory (A=%s, B=%s)",
			describeKind(aIsDir), describeKind(bIsDir))
	}

	if !aIsDir {
		return diffSingleFile(opt.A, opt.B, opt.Mode)
	}

	if !opt.Recursive {
		return fmt.Errorf("diff: both sides are directories; pass --recursive")
	}
	return diffDirectories(opt)
}

func describeKind(isDir bool) string {
	if isDir {
		return "directory"
	}
	return "file"
}

// endpointIsDir 判断一个端点是“目录”还是“文件”。
//
//   - 本地：用 os.Stat
//   - s3：用 HeadObject + 退化 List
//
// 不存在则返回错误。
func endpointIsDir(e *DiffEndpoint) (bool, error) {
	if !e.IsS3 {
		info, err := os.Stat(e.Path)
		if err != nil {
			return false, err
		}
		return info.IsDir(), nil
	}

	if e.Key == "" || e.TrailingSlash {
		return true, nil
	}
	// 先 head，失败再 list
	ctx := e.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	s3client := &S3Client{S3: e.S3, Ctx: ctx}
	isFile, err := s3client.IsS3File(e.Bucket, e.Key)
	if err == nil {
		return !isFile, nil
	}
	return false, err
}

// =============== 单文件 diff ===============

func diffSingleFile(a, b *DiffEndpoint, mode DiffMode) error {
	ea, err := statOneFile(a, "")
	if err != nil {
		return fmt.Errorf("stat A: %w", err)
	}
	eb, err := statOneFile(b, "")
	if err != nil {
		return fmt.Errorf("stat B: %w", err)
	}

	if ea.Size != eb.Size {
		myprint.PrintfRed("DIFFER  %s  vs  %s  (size %s vs %s)\n",
			a.String(), b.String(),
			FormatBytes(ea.Size), FormatBytes(eb.Size))
		return errDiffer
	}

	if mode == DiffModeSize {
		myprint.PrintfGreen("OK      %s  vs  %s  (size %s)\n",
			a.String(), b.String(), FormatBytes(ea.Size))
		return nil
	}
	if mode == DiffModeQuick {
		if ea.Mtime != eb.Mtime {
			myprint.PrintfRed("DIFFER  %s  vs  %s  (mtime %d vs %d)\n",
				a.String(), b.String(), ea.Mtime, eb.Mtime)
			return errDiffer
		}
		myprint.PrintfGreen("OK      %s  vs  %s  (size %s, mtime match)\n",
			a.String(), b.String(), FormatBytes(ea.Size))
		return nil
	}

	// MD5 模式：流式对比
	equal, err := compareContent(a, "", b, "")
	if err != nil {
		return err
	}
	if !equal {
		myprint.PrintfRed("DIFFER  %s  vs  %s  (content)\n", a.String(), b.String())
		return errDiffer
	}
	myprint.PrintfGreen("OK      %s  vs  %s  (size %s, md5 match)\n",
		a.String(), b.String(), FormatBytes(ea.Size))
	return nil
}

// errDiffer 用于让上层（命令）以非零退出码退出。
var errDiffer = fmt.Errorf("differences found")

// IsDifferErr 命令层用它来识别“存在差异”这一非错误异常。
func IsDifferErr(err error) bool { return err == errDiffer }

// =============== 目录 diff ===============

func diffDirectories(opt DiffOptions) error {

	listA, err := listAllEntries(opt.A)
	if err != nil {
		return fmt.Errorf("list A: %w", err)
	}

	listB, err := listAllEntries(opt.B)
	if err != nil {
		return fmt.Errorf("list B: %w", err)
	}

	mapA := indexBy(listA)
	mapB := indexBy(listB)

	var onlyA, onlyB, identical, differ []string
	var (
		mu     sync.Mutex
		failed atomic.Int64
		wg     sync.WaitGroup
		sem    = make(chan struct{}, opt.Concurrency)
	)

	// 1. 在 A 里：标记 only-A 或进入内容比较
	for rel, ea := range mapA {
		eb, ok := mapB[rel]
		if !ok {
			onlyA = append(onlyA, rel)
			continue
		}
		// size 先比
		if ea.Size != eb.Size {
			differ = append(differ, fmt.Sprintf("%s  (size %s vs %s)",
				rel, FormatBytes(ea.Size), FormatBytes(eb.Size)))
			continue
		}
		switch opt.Mode {
		case DiffModeSize:
			identical = append(identical, rel)
		case DiffModeQuick:
			if ea.Mtime != eb.Mtime {
				differ = append(differ, fmt.Sprintf("%s  (mtime %d vs %d)", rel, ea.Mtime, eb.Mtime))
			} else {
				identical = append(identical, rel)
			}
		case DiffModeMD5:
			// 并发做 MD5 比对
			wg.Add(1)
			sem <- struct{}{}
			go func(rel string) {
				defer wg.Done()
				defer func() { <-sem }()
				equal, err := compareContent(opt.A, rel, opt.B, rel)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					failed.Add(1)
					differ = append(differ, fmt.Sprintf("%s  (error: %v)", rel, err))
					return
				}
				if equal {
					identical = append(identical, rel)
				} else {
					differ = append(differ, fmt.Sprintf("%s  (content)", rel))
				}
			}(rel)
		}
	}

	// 2. 找出 only-B
	for rel := range mapB {
		if _, ok := mapA[rel]; !ok {
			onlyB = append(onlyB, rel)
		}
	}

	wg.Wait()

	sort.Strings(onlyA)
	sort.Strings(onlyB)
	sort.Strings(identical)
	sort.Strings(differ)

	// 3. 打印
	myprint.PrintfDim("--- A: %s\n", opt.A.String())
	myprint.PrintfDim("+++ B: %s\n", opt.B.String())
	myprint.PrintfDim("mode=%s, concurrency=%d\n", opt.Mode, opt.Concurrency)
	myprint.Println()
	for _, k := range differ {
		myprint.PrintfRed("DIFFER %s\n", k)
	}
	for _, k := range onlyA {
		myprint.PrintfYellow("ONLY-A %s\n", k)
	}
	for _, k := range onlyB {
		myprint.PrintfYellow("ONLY-B %s\n", k)
	}
	if !opt.BriefOnly {
		for _, k := range identical {
			myprint.PrintfGreen("OK     %s\n", k)
		}
	}

	myprint.Println()
	myprint.PrintfBoldCyan("Summary: identical=%d differ=%d only-A=%d only-B=%d\n",
		len(identical), len(differ), len(onlyA), len(onlyB))

	if len(differ)+len(onlyA)+len(onlyB) > 0 {
		return errDiffer
	}
	if failed.Load() > 0 {
		return fmt.Errorf("%d files failed to compare", failed.Load())
	}
	return nil
}

func indexBy(list []fileEntry) map[string]fileEntry {
	m := make(map[string]fileEntry, len(list))
	for _, e := range list {
		m[e.Path] = e
	}
	return m
}

// =============== 列举 / stat ===============

// listAllEntries 列出端点下的全部“文件”，相对路径作为 fileEntry.Path。
func listAllEntries(e *DiffEndpoint) ([]fileEntry, error) {
	if !e.IsS3 {
		return listLocalDir(e.Path)
	}
	return listS3Dir(e.S3, e.Ctx, e.Alias, e.Bucket, e.Key)
}

func listLocalDir(root string) ([]fileEntry, error) {
	var out []fileEntry
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		// 用 "/" 做相对路径分隔，跨平台对齐 s3
		rel = filepath.ToSlash(rel)
		out = append(out, fileEntry{
			Path:  rel,
			Size:  info.Size(),
			Mtime: info.ModTime().Unix(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func listS3Dir(cli *s3.Client, ctx context.Context, alias, bucket, prefix string) ([]fileEntry, error) {
	// 规范化 prefix，确保 "目录" 风格
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	paginator := s3.NewListObjectsV2Paginator(cli, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket), Prefix: aws.String(prefix),
	})
	var out []fileEntry
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list %s: %s", S3PathStatic(alias, bucket, prefix), FormatAPIError(err))
		}
		for _, obj := range page.Contents {
			key := aws.ToString(obj.Key)
			// 跳过 "目录占位符"
			if strings.HasSuffix(key, "/") && aws.ToInt64(obj.Size) == 0 {
				continue
			}
			rel := strings.TrimPrefix(key, prefix)
			if rel == "" {
				continue
			}
			out = append(out, fileEntry{
				Path:  rel,
				Size:  aws.ToInt64(obj.Size),
				Mtime: aws.ToTime(obj.LastModified).Unix(),
				ETag:  strings.Trim(aws.ToString(obj.ETag), `"`),
			})
		}
	}
	return out, nil
}

// statOneFile 取一个端点下“单个文件”的元信息。
// 在目录模式下被复用：rel 为子相对路径（“”表示端点自身就是文件）。
func statOneFile(e *DiffEndpoint, rel string) (fileEntry, error) {
	if !e.IsS3 {
		path := e.Path
		if rel != "" {
			path = filepath.Join(e.Path, filepath.FromSlash(rel))
		}
		info, err := os.Stat(path)
		if err != nil {
			return fileEntry{}, err
		}
		if info.IsDir() {
			return fileEntry{}, fmt.Errorf("%s: is a directory", path)
		}
		return fileEntry{Path: rel, Size: info.Size(), Mtime: info.ModTime().Unix()}, nil
	}

	key := e.Key
	if rel != "" {
		if !strings.HasSuffix(key, "/") && key != "" {
			key += "/"
		}
		key += rel
	}
	out, err := e.S3.HeadObject(e.Ctx, &s3.HeadObjectInput{
		Bucket: aws.String(e.Bucket), Key: aws.String(key),
	})
	if err != nil {
		return fileEntry{}, fmt.Errorf("head %s: %s", S3PathStatic(e.Alias, e.Bucket, key), FormatAPIError(err))
	}
	mtime := int64(0)
	if out.LastModified != nil {
		mtime = out.LastModified.Unix()
	}
	return fileEntry{
		Path:  rel,
		Size:  aws.ToInt64(out.ContentLength),
		Mtime: mtime,
		ETag:  strings.Trim(aws.ToString(out.ETag), `"`),
	}, nil
}

// =============== 内容比较（MD5 / 流式） ===============

// compareContent 流式打开两端，分块读，分别更新 MD5；
// 任一边读完后比较 hash。size 已保证相等。
func compareContent(a *DiffEndpoint, relA string, b *DiffEndpoint, relB string) (bool, error) {
	ra, err := openReader(a, relA)
	if err != nil {
		return false, err
	}
	defer ra.Close()

	rb, err := openReader(b, relB)
	if err != nil {
		return false, err
	}
	defer rb.Close()

	const bufSize = 1 << 20 // 1MB
	bufA := make([]byte, bufSize)
	bufB := make([]byte, bufSize)
	hA := md5.New()
	hB := md5.New()

	for {
		nA, errA := io.ReadFull(ra, bufA)
		nB, errB := io.ReadFull(rb, bufB)

		if nA != nB {
			return false, nil
		}
		if nA > 0 {
			hA.Write(bufA[:nA])
			hB.Write(bufB[:nB])
			// 提前比较 chunk，避免读完整个大文件后才发现不同
			if !bytes.Equal(bufA[:nA], bufB[:nB]) {
				return false, nil
			}
		}

		// 处理结束条件
		endA := errA == io.EOF || errA == io.ErrUnexpectedEOF
		endB := errB == io.EOF || errB == io.ErrUnexpectedEOF
		if endA && endB {
			return hex.EncodeToString(hA.Sum(nil)) == hex.EncodeToString(hB.Sum(nil)), nil
		}
		if endA != endB {
			return false, nil
		}
		if errA != nil && !endA {
			return false, fmt.Errorf("read A: %w", errA)
		}
		if errB != nil && !endB {
			return false, fmt.Errorf("read B: %w", errB)
		}
	}
}

// openReader 打开一端对应的相对路径，返回 io.ReadCloser。
func openReader(e *DiffEndpoint, rel string) (io.ReadCloser, error) {
	if !e.IsS3 {
		path := e.Path
		if rel != "" {
			path = filepath.Join(e.Path, filepath.FromSlash(rel))
		}
		return os.Open(path)
	}
	key := e.Key
	if rel != "" {
		if !strings.HasSuffix(key, "/") && key != "" {
			key += "/"
		}
		key += rel
	}
	out, err := e.S3.GetObject(e.Ctx, &s3.GetObjectInput{
		Bucket: aws.String(e.Bucket), Key: aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("get %s: %s", S3PathStatic(e.Alias, e.Bucket, key), FormatAPIError(err))
	}
	return out.Body, nil
}
