package action

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"s3cli/pkg/s3api"
)

func TestIsCanceled(t *testing.T) {
	if IsCanceled(nil) {
		t.Error("nil should not be canceled")
	}
	if !IsCanceled(context.Canceled) {
		t.Error("context.Canceled should be detected")
	}
	if !IsCanceled(context.DeadlineExceeded) {
		t.Error("DeadlineExceeded should be detected")
	}
	if !IsCanceled(errors.New("something: context canceled")) {
		t.Error("text fallback should detect 'context canceled'")
	}
	if IsCanceled(errors.New("network error")) {
		t.Error("unrelated error not canceled")
	}
}

func TestFormatAPIError(t *testing.T) {
	if FormatAPIError(nil) != "" {
		t.Error("nil should give empty")
	}
	apiErr := &s3api.ErrorResponse{Code: "NoSuchKey", Message: "gone"}
	if got := FormatAPIError(apiErr); got != "NoSuchKey: gone" {
		t.Errorf("got %q", got)
	}
	plain := errors.New("boom")
	if got := FormatAPIError(plain); got != "boom" {
		t.Errorf("got %q", got)
	}
}

func TestAddMime(t *testing.T) {
	AddMime()
	AddMime() // 幂等, 不应 panic
	if ct := detectMime("video.mp4", ""); ct != "video/mp4" {
		t.Errorf("mp4 -> %q", ct)
	}
	if ct := detectMime("unknown.zzzznope", "custom/x"); ct != "custom/x" {
		t.Errorf("fallback default -> %q", ct)
	}
	if ct := detectMime("unknown.zzzznope", ""); ct != "binary/octet-stream" {
		t.Errorf("empty default -> %q", ct)
	}
}

func TestGlobToRegex(t *testing.T) {
	cases := []struct {
		glob, line string
		match      bool
	}{
		{"*.txt", "a.txt", true},
		{"*.txt", "a.csv", false},
		{"a?c", "abc", true},
		{"a?c", "ac", false},
		{"file.[ch]", "file.c", true},
		{"file.[ch]", "file.h", true},
		{"file.[ch]", "file.d", false},
		{"[!x]*", "abc", true},
		{"[!x]*", "xbc", false},
		{"a.b", "a.b", true},
		{"a.b", "axb", false}, // '.' 被转义
	}
	for _, tc := range cases {
		pat := globToRegex(tc.glob)
		re, err := regexp.Compile(pat)
		if err != nil {
			t.Errorf("globToRegex(%q) produced invalid regex %q: %v", tc.glob, pat, err)
			continue
		}
		if re.MatchString(tc.line) != tc.match {
			t.Errorf("globToRegex(%q)=%q match %q = %v, want %v", tc.glob, pat, tc.line, !tc.match, tc.match)
		}
	}
}

func TestParseTime(t *testing.T) {
	good := []string{
		"2024-01-02",
		"2024-01-02 15:04:05",
		"2024-01-02T15:04:05Z",
		"2024-01-02T15:04:05+08:00",
	}
	for _, s := range good {
		if _, err := parseTime(s); err != nil {
			t.Errorf("parseTime(%q) error: %v", s, err)
		}
	}
	if _, err := parseTime("garbage"); err == nil {
		t.Error("expected error for garbage")
	}
	if _, err := parseTime(""); err == nil {
		t.Error("expected error for empty")
	}
}

func TestDetermineLocalFilePath(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "exists.txt")
	os.WriteFile(existing, []byte("x"), 0o644)

	t.Run("empty local -> basename", func(t *testing.T) {
		got, err := determineLocalFilePath("", "prefix/key.txt")
		if err != nil || got != "key.txt" {
			t.Errorf("got %q err %v", got, err)
		}
	})
	t.Run("existing dir -> join", func(t *testing.T) {
		got, err := determineLocalFilePath(dir, "k/obj")
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(dir, "obj")
		if got != want {
			t.Errorf("got %q want %q", got, want)
		}
	})
	t.Run("existing file -> as-is", func(t *testing.T) {
		got, err := determineLocalFilePath(existing, "k/obj")
		if err != nil || got != existing {
			t.Errorf("got %q err %v", got, err)
		}
	})
	t.Run("nonexistent parent missing -> error", func(t *testing.T) {
		_, err := determineLocalFilePath(filepath.Join(dir, "nodir", "sub", "f"), "k/obj")
		if err == nil {
			t.Error("expected error for missing parent")
		}
	})
}

func TestDetermineLocalBasePath(t *testing.T) {
	dir := t.TempDir()
	t.Run("existing dir", func(t *testing.T) {
		got, err := determineLocalBasePath(dir, "b", "")
		if err != nil || got != dir {
			t.Errorf("got %q err %v", got, err)
		}
	})
	t.Run("empty + key -> base(key)", func(t *testing.T) {
		got, _ := determineLocalBasePath("", "b", "prefix/name/")
		if got != "name" {
			t.Errorf("got %q want name", got)
		}
	})
	t.Run("empty + no key -> bucket", func(t *testing.T) {
		got, _ := determineLocalBasePath("", "mybucket", "")
		if got != "mybucket" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("existing file -> error", func(t *testing.T) {
		f := filepath.Join(dir, "afile")
		os.WriteFile(f, []byte("x"), 0o644)
		_, err := determineLocalBasePath(f, "b", "")
		if err == nil {
			t.Error("expected error: not a directory")
		}
	})
}

func TestBuildLocalFilePath(t *testing.T) {
	t.Run("no prefix", func(t *testing.T) {
		got, err := buildLocalFilePath("a/b/c.txt", "", "/tmp/base")
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join("/tmp/base", "a", "b", "c.txt")
		if got != want {
			t.Errorf("got %q want %q", got, want)
		}
	})
	t.Run("strip prefix", func(t *testing.T) {
		got, _ := buildLocalFilePath("prefix/sub/c.txt", "prefix/", "/base")
		want := filepath.Join("/base", "sub", "c.txt")
		if got != want {
			t.Errorf("got %q want %q", got, want)
		}
	})
	t.Run("key equals prefix -> base", func(t *testing.T) {
		got, _ := buildLocalFilePath("prefix/", "prefix", "/base")
		if got != "/base" {
			t.Errorf("got %q", got)
		}
	})
}

func TestSafeJoinLocalRejectsTraversal(t *testing.T) {
	if _, err := safeJoinLocal("/base", "a/../b"); err == nil {
		t.Error("expected error for '..' segment")
	}
	got, err := safeJoinLocal("/base", "sub/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("/base", "sub", "file.txt")
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestCountingReader(t *testing.T) {
	var total int64
	cr := &countingReader{r: strings.NewReader("hello"), report: func(n int64) { total += n }}
	buf := make([]byte, 3)
	cr.Read(buf)
	if total != 3 {
		t.Errorf("reported %d, want 3", total)
	}
	// nil report 不 panic
	cr2 := &countingReader{r: strings.NewReader("hi")}
	if _, err := cr2.Read(buf); err != nil {
		t.Error(err)
	}
}

func TestRoundUpToBlock(t *testing.T) {
	cases := []struct{ size, block, want int64 }{
		{0, 4, 0},   // size<=0
		{10, 0, 10}, // block<=0
		{10, 4, 12},
		{8, 4, 8}, // 精确倍数
		{1, 1024, 1024},
	}
	for _, tc := range cases {
		if got := roundUpToBlock(tc.size, tc.block); got != tc.want {
			t.Errorf("roundUpToBlock(%d,%d)=%d want %d", tc.size, tc.block, got, tc.want)
		}
	}
}

func TestParentDirectory(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"file.txt", ""},
		{"dir/file.txt", "dir/"},
		{"a/b/c/", "a/b/"},
		{"a/b/c", "a/b/"},
	}
	for _, tc := range cases {
		if got := parentDirectory(tc.in); got != tc.want {
			t.Errorf("parentDirectory(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeMirrorPrefix(t *testing.T) {
	if got := normalizeMirrorPrefix("dir"); got != "dir/" {
		t.Errorf("got %q", got)
	}
	if got := normalizeMirrorPrefix("dir/"); got != "dir/" {
		t.Errorf("got %q", got)
	}
	if got := normalizeMirrorPrefix(""); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestJoinKey(t *testing.T) {
	cases := []struct{ prefix, rel, want string }{
		{"", "a/b", "a/b"},
		{"dir/", "a", "dir/a"},
		{"dir", "a", "dir/a"},
		{"p/", "", "p/"},
	}
	for _, tc := range cases {
		if got := joinKey(tc.prefix, tc.rel); got != tc.want {
			t.Errorf("joinKey(%q,%q)=%q want %q", tc.prefix, tc.rel, got, tc.want)
		}
	}
}

func TestRelKey(t *testing.T) {
	if got := relKey("pre/key", "pre"); got != "/key" {
		t.Errorf("got %q", got)
	}
	if got := relKey("key", "pre"); got != "key" {
		t.Errorf("got %q", got)
	}
}

func TestMatchMirrorGlob(t *testing.T) {
	if !matchMirrorGlob("*.txt", "a/b.txt") {
		t.Error("*.txt should match a/b.txt (basename)")
	}
	if !matchMirrorGlob("*.txt", "b.txt") {
		t.Error("*.txt should match b.txt")
	}
	if matchMirrorGlob("*.txt", "a.csv") {
		t.Error("should not match a.csv")
	}
	// 带 / 的 pattern 只匹配全路径
	if !matchMirrorGlob("dir/*", "dir/file") {
		t.Error("dir/* should match dir/file")
	}
	if matchMirrorGlob("dir/*", "other/file") {
		t.Error("dir/* should not match other/file")
	}
}

func TestNeedsUpdateEdges(t *testing.T) {
	now := time.Now()
	t1 := ObjectInfo{ETag: "abc", Size: 10, LastModified: now}
	t2 := ObjectInfo{ETag: "abc", Size: 10, LastModified: now}
	if needsUpdate(t1, t2) {
		t.Error("identical etag should not need update")
	}
	t2b := ObjectInfo{ETag: "xyz", Size: 10, LastModified: now}
	if !needsUpdate(t1, t2b) {
		t.Error("differing etag should need update")
	}
	// MPU etag (含 '-') 退化到 size+mtime
	src := ObjectInfo{ETag: "abc-1", Size: 10, LastModified: now.Add(time.Second)}
	tgt := ObjectInfo{ETag: "def-1", Size: 10, LastModified: now}
	if !needsUpdate(src, tgt) {
		t.Error("newer src should need update")
	}
	srcSame := ObjectInfo{ETag: "abc-1", Size: 10, LastModified: now}
	if needsUpdate(srcSame, tgt) {
		t.Error("same size+mtime should not need update")
	}
}

func TestBuildDestKey(t *testing.T) {
	t.Run("no append", func(t *testing.T) {
		if got := buildDestKey("a/b", "a/", "dst", false); got != "dst" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("append with prefix strip", func(t *testing.T) {
		if got := buildDestKey("src/sub/f.txt", "src/", "dst", true); got != "dst/sub/f.txt" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("append empty rel", func(t *testing.T) {
		if got := buildDestKey("src/", "src", "dst", true); got != "dst" {
			t.Errorf("got %q", got)
		}
	})
}

func TestParseTagPairs(t *testing.T) {
	m := map[string]string{"k1": "v1", "k2": "v2"}
	tags := parseTagPairs(m)
	if len(tags) != 2 {
		t.Fatalf("got %d tags", len(tags))
	}
	seen := map[string]string{}
	for _, tg := range tags {
		seen[tg.Key] = tg.Value
	}
	if seen["k1"] != "v1" || seen["k2"] != "v2" {
		t.Errorf("unexpected: %+v", seen)
	}
	if len(parseTagPairs(nil)) != 0 {
		t.Error("nil should give empty")
	}
}

func TestS3PathFormatting(t *testing.T) {
	c := &S3Client{Alias: "prod"}
	if got := c.S3Path("bucket", ""); got != "prod:bucket" {
		t.Errorf("got %q", got)
	}
	if got := c.S3Path("bucket", "key"); got != "prod:bucket/key" {
		t.Errorf("got %q", got)
	}
	if got := S3PathStatic("prod", "bucket", ""); got != "prod:bucket" {
		t.Errorf("got %q", got)
	}
	if got := S3PathStatic("prod", "bucket", "k"); got != "prod:bucket/k" {
		t.Errorf("got %q", got)
	}
}

func TestTreeNodeInsertAndSort(t *testing.T) {
	root := &treeNode{name: "", children: map[string]*treeNode{}}
	root.insert(strings.Split("dir/sub/file.txt", "/"), 100)
	root.insert(strings.Split("dir/file2.txt", "/"), 50)
	root.insert(strings.Split("ztop.txt", "/"), 10)

	// 空插入安全
	root.insert(nil, 0)

	kids := root.sortedChildren()
	if len(kids) != 2 {
		t.Fatalf("expected 2 top-level children, got %d", len(kids))
	}
	// 目录(dir)应在文件(ztop.txt)之前
	if kids[0].name != "dir" {
		t.Errorf("expected dir first, got %q", kids[0].name)
	}
	if kids[1].name != "ztop.txt" {
		t.Errorf("expected ztop.txt second, got %q", kids[1].name)
	}
	// 检查 dir 的子节点排序: 目录(sub)在文件(file2.txt)前
	dirKids := kids[0].sortedChildren()
	if len(dirKids) != 2 {
		t.Fatalf("expected 2 dir children, got %d", len(dirKids))
	}
	if dirKids[0].name != "sub" {
		t.Errorf("expected subdir 'sub' first, got %q", dirKids[0].name)
	}
	if dirKids[1].name != "file2.txt" {
		t.Errorf("expected file 'file2.txt' second, got %q", dirKids[1].name)
	}
}

func TestDescribeKind(t *testing.T) {
	if describeKind(true) != "directory" {
		t.Error("dir")
	}
	if describeKind(false) != "file" {
		t.Error("file")
	}
}

func TestDrainListErr(t *testing.T) {
	// 空通道 -> nil
	if err := drainListErr(nil); err != nil {
		t.Errorf("nil chan should give nil, got %v", err)
	}
	// 取消错误 -> nil
	ch := make(chan error, 1)
	ch <- context.Canceled
	if err := drainListErr(ch); err != nil {
		t.Errorf("canceled should give nil, got %v", err)
	}
	// 真实错误 -> 返回
	ch2 := make(chan error, 1)
	real := errors.New("list failed")
	ch2 <- real
	if err := drainListErr(ch2); err != real {
		t.Errorf("expected real error, got %v", err)
	}
}

func TestIndexBy(t *testing.T) {
	list := []fileEntry{
		{Path: "a", Size: 1},
		{Path: "b", Size: 2},
	}
	m := indexBy(list)
	if len(m) != 2 || m["a"].Size != 1 || m["b"].Size != 2 {
		t.Errorf("unexpected map: %+v", m)
	}
}

func TestDiffEndpointString(t *testing.T) {
	s3e := &DiffEndpoint{IsS3: true, Alias: "p", Bucket: "b", Key: "k"}
	if got := s3e.String(); got != "p:b/k" {
		t.Errorf("got %q", got)
	}
	local := &DiffEndpoint{IsS3: false, Path: "/tmp/x"}
	if got := local.String(); got != "/tmp/x" {
		t.Errorf("got %q", got)
	}
}
