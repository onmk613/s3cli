// object-cat.go 实现对象内容输出到 stdout (CatObject), 参数与 mc cat 对齐:
// --offset/-o, --tail/-t, --version-id/--vid, --range (兼容旧参数);
// --lines/-n 支持 head 场景 (仅输出前 N 行).

package action

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"s3cli/pkg/s3iface"
)

// CatOptions cat/head 命令参数
type CatOptions struct {
	Range     string // HTTP Range header (e.g. "bytes=0-1023")
	Offset    int64  // -o: 起始偏移 (字节)
	Tail      int64  // -t: 只输出末尾 N 字节
	VersionID string // --version-id/--vid: 指定对象版本
	Lines     int    // -n: 只输出前 N 行 (head)
}

func (c *Action) CatObject(opt CatOptions, bucket, key string) error {
	if key == "" {
		return fmt.Errorf("cat requires an object key, not a bucket")
	}

	opts := &s3iface.GetObjectOptions{
		VersionID: opt.VersionID,
	}
	if rng := buildCatRange(opt); rng != "" {
		opts.Range = rng
	}

	out, err := c.S3.GetObject(c.Ctx, bucket, key, opts)
	if err != nil {
		return fmt.Errorf("cat %s: %s", c.S3Path(bucket, key), FormatAPIError(err))
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

// buildCatRange 由 --offset/--tail/--range 计算 Range 头.
func buildCatRange(opt CatOptions) string {
	if opt.Range != "" {
		return opt.Range
	}
	if opt.Tail > 0 {
		// 末尾 N 字节: 服务端支持 "-N" 语法; 部分实现只认 "bytes=N-" 时回退用
		// "bytes=-N"。S3 标准支持 suffix range。
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
