package action

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

// siblingPrefixServer 模拟一个同时存在 "logs/" 与 "logs-2023/" 的桶,
// 记录每次列举用的 prefix 以及全部写操作 (复制/删除)。
//
// 这是"裸前缀前缀碰撞"的最小复现环境: "logs" 做纯字符串前缀匹配会连带
// 命中 "logs-2023/x.txt"。
type siblingPrefixServer struct {
	srv *httptest.Server

	mu         sync.Mutex
	listPrefix []string
	copiedTo   []string
	deleted    []string
}

func newSiblingPrefixServer(t *testing.T) *siblingPrefixServer {
	t.Helper()
	s := &siblingPrefixServer{}
	s.srv = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *siblingPrefixServer) keys() []string {
	return []string{"logs-2023/x.txt", "logs/a.txt", "logs/sub/b.txt"}
}

func (s *siblingPrefixServer) handle(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	switch {
	case r.Method == http.MethodHead:
		httpError(w, http.StatusNotFound, "NoSuchKey", "not found")
	case q.Get("list-type") == "2":
		prefix := q.Get("prefix")
		s.mu.Lock()
		s.listPrefix = append(s.listPrefix, prefix)
		s.mu.Unlock()

		var matched []string
		for _, k := range s.keys() {
			if strings.HasPrefix(k, prefix) {
				matched = append(matched, k)
			}
		}
		sort.Strings(matched)
		var b strings.Builder
		b.WriteString(`<ListBucketResult><IsTruncated>false</IsTruncated>`)
		for _, k := range matched {
			fmt.Fprintf(&b, `<Contents><Key>%s</Key><Size>5</Size></Contents>`, k)
		}
		b.WriteString(`</ListBucketResult>`)
		_, _ = io.WriteString(w, b.String())
	case r.Method == http.MethodPut && r.Header.Get("x-amz-copy-source") != "":
		s.mu.Lock()
		s.copiedTo = append(s.copiedTo, r.URL.Path)
		s.mu.Unlock()
		_, _ = io.WriteString(w, `<CopyObjectResult><ETag>"e"</ETag></CopyObjectResult>`)
	case r.Method == http.MethodDelete:
		s.mu.Lock()
		s.deleted = append(s.deleted, r.URL.Path)
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodGet:
		_, _ = io.WriteString(w, "hello")
	default:
		httpError(w, http.StatusBadRequest, "InvalidRequest", "unsupported")
	}
}

func (s *siblingPrefixServer) snapshot() (prefixes, copied, deleted []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.listPrefix...),
		append([]string(nil), s.copiedTo...),
		append([]string(nil), s.deleted...)
}

// assertNoSiblingTouched 断言整个过程中的列举与写操作都没碰到 "logs-2023"。
func (s *siblingPrefixServer) assertNoSiblingTouched(t *testing.T) {
	t.Helper()
	prefixes, copied, deleted := s.snapshot()
	for _, p := range prefixes {
		if p == "logs" {
			t.Errorf("listed with bare prefix %q; a directory source must list with %q "+
				"(bare prefix string-matches the sibling directory logs-2023/)", p, "logs/")
		}
	}
	for _, c := range append(append([]string(nil), copied...), deleted...) {
		if strings.Contains(c, "logs-2023") {
			t.Errorf("sibling prefix was touched: %s (all copied=%v deleted=%v)", c, copied, deleted)
		}
	}
	// 子串匹配同样要盯住 "-2023": 修复前的目标 key 是
	// "archive/logs/-2023/x.txt" —— 不含 "logs-2023", 但明显是剥错前缀的产物。
	for _, c := range copied {
		if strings.Contains(c, "-2023") {
			t.Errorf("destination key derived from a sibling object: %s", c)
		}
	}
}

// TestMvRecursiveDoesNotTouchSiblingPrefix 是回归测试。
//
// 修复前 `mv -r a:b/logs a:b/archive/` 会用裸前缀 "logs" 列举, 连带把
// "logs-2023/x.txt" 复制成 "archive/logs/-2023/x.txt", 并删除源对象
// "logs-2023/x.txt" —— 一次移动即毁掉无关前缀的数据。
func TestMvRecursiveDoesNotTouchSiblingPrefix(t *testing.T) {
	s := newSiblingPrefixServer(t)
	a := &Action{S3: actionTestClient(t, s.srv.URL, nil), Alias: "test", Ctx: context.Background()}

	if err := a.Mv(CopyOptions{Recursive: true, NoProgress: true}, "bucket", "logs", "bucket", "archive/"); err != nil {
		t.Fatal(err)
	}
	s.assertNoSiblingTouched(t)

	_, _, deleted := s.snapshot()
	sort.Strings(deleted)
	want := []string{"/bucket/logs/a.txt", "/bucket/logs/sub/b.txt"}
	if strings.Join(deleted, ",") != strings.Join(want, ",") {
		t.Fatalf("deleted = %v, want exactly %v", deleted, want)
	}
}

// TestCopyRecursiveDoesNotTouchSiblingPrefix 同上, 覆盖 cp。
func TestCopyRecursiveDoesNotTouchSiblingPrefix(t *testing.T) {
	s := newSiblingPrefixServer(t)
	a := &Action{S3: actionTestClient(t, s.srv.URL, nil), Alias: "test", Ctx: context.Background()}

	if err := a.CopyObjects(CopyOptions{Recursive: true, NoProgress: true}, "bucket", "logs", "bucket", "archive/"); err != nil {
		t.Fatal(err)
	}
	s.assertNoSiblingTouched(t)

	_, copied, _ := s.snapshot()
	sort.Strings(copied)
	want := []string{"/bucket/archive/logs/a.txt", "/bucket/archive/logs/sub/b.txt"}
	if strings.Join(copied, ",") != strings.Join(want, ",") {
		t.Fatalf("copied = %v, want exactly %v", copied, want)
	}
}

// TestGetRecursiveDoesNotWriteSiblingPaths 覆盖 get: 修复前会写出
// "<base>/-2023/x.txt" 这种由剥错前缀产生的本地垃圾路径。
func TestGetRecursiveDoesNotWriteSiblingPaths(t *testing.T) {
	s := newSiblingPrefixServer(t)
	a := &Action{S3: actionTestClient(t, s.srv.URL, nil), Alias: "test", Ctx: context.Background()}

	dest := t.TempDir()
	if err := a.GetObject(GetOptions{Recursive: true, NoProgress: true}, "bucket", "logs", dest); err != nil {
		t.Fatal(err)
	}
	s.assertNoSiblingTouched(t)

	var got []string
	err := filepath.Walk(dest, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, relErr := filepath.Rel(dest, p)
		if relErr != nil {
			return relErr
		}
		got = append(got, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	want := []string{"a.txt", "sub/b.txt"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("downloaded files = %v, want %v", got, want)
	}
}

// TestNormalizeDirPrefixIsAppliedToDirSources 锁定规范化函数本身的行为。
func TestNormalizeDirPrefixIsAppliedToDirSources(t *testing.T) {
	cases := map[string]string{
		"logs":   "logs/",
		"logs/":  "logs/",
		"a/b":    "a/b/",
		"":       "", // 桶根: 不能变成 "/", 否则列举会带前导斜杠
		"a/b/c/": "a/b/c/",
	}
	for in, want := range cases {
		if got := normalizeDirPrefix(in); got != want {
			t.Errorf("normalizeDirPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}
