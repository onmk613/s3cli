// mirror_flow_test.go 覆盖 Mirror 主流程 (此前 Mirror / streamObjects / dryRun
// 均为 0% 覆盖)。
//
// mirror 是 README 的头号同步特性, 而且它的"复制阶段"与"删除阶段"之间有数据
// 安全围栏: 只要有任何一个对象复制失败, 就绝不能进入 --remove 的删除阶段 ——
// 否则一次网络抖动就会把目标端"看起来多余"的对象成批删掉, 而那些对象本该被
// 失败的那次复制更新。本文件把这个围栏锁进测试。

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

	"s3cli/internal/api"
)

// mirrorServer 是一个双 bucket 的内存 S3 服务端, 支持 mirror 需要的
// ListObjectsV2 / HEAD / GET / PUT(CopyObject) / DELETE。
type mirrorServer struct {
	srv *httptest.Server

	mu       sync.Mutex
	objects  map[string]string // "bucket/key" -> body
	failCopy map[string]bool   // "bucket/key" (目标) 命中时 CopyObject 返回 500
	deleted  []string
	copied   []string
}

func newMirrorServer(t *testing.T, objects map[string]string) *mirrorServer {
	t.Helper()
	m := &mirrorServer{objects: objects, failCopy: map[string]bool{}}
	m.srv = httptest.NewServer(http.HandlerFunc(m.handle))
	t.Cleanup(m.srv.Close)
	return m
}

// splitBucketKey 从 URL path 拆出 bucket/key (path-style)。
func splitBucketKey(p string) (string, string) {
	p = strings.TrimPrefix(p, "/")
	if i := strings.Index(p, "/"); i >= 0 {
		return p[:i], p[i+1:]
	}
	return p, ""
}

func (m *mirrorServer) handle(w http.ResponseWriter, r *http.Request) {
	bucket, key := splitBucketKey(r.URL.Path)
	q := r.URL.Query()

	m.mu.Lock()
	defer m.mu.Unlock()

	switch {
	case q.Get("list-type") == "2":
		prefix := q.Get("prefix")
		var keys []string
		for k := range m.objects {
			b, objKey, _ := strings.Cut(k, "/")
			if b == bucket && strings.HasPrefix(objKey, prefix) {
				keys = append(keys, objKey)
			}
		}
		sort.Strings(keys)
		var b strings.Builder
		b.WriteString(`<ListBucketResult><IsTruncated>false</IsTruncated>`)
		for _, k := range keys {
			fmt.Fprintf(&b, `<Contents><Key>%s</Key><Size>%d</Size><ETag>&quot;etag-%s&quot;</ETag></Contents>`,
				k, len(m.objects[bucket+"/"+k]), k)
		}
		b.WriteString(`</ListBucketResult>`)
		_, _ = io.WriteString(w, b.String())

	case r.Method == http.MethodHead:
		body, ok := m.objects[bucket+"/"+key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		w.Header().Set("ETag", `"etag-`+key+`"`)
		w.Header().Set("Last-Modified", "Wed, 21 Oct 2026 07:28:00 GMT")
		w.WriteHeader(http.StatusOK)

	case r.Method == http.MethodGet:
		body, ok := m.objects[bucket+"/"+key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		w.Header().Set("ETag", `"etag-`+key+`"`)
		_, _ = io.WriteString(w, body)

	case r.Method == http.MethodPut && r.Header.Get("x-amz-copy-source") != "":
		if m.failCopy[bucket+"/"+key] {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `<Error><Code>InternalError</Code><Message>copy failed</Message></Error>`)
			return
		}
		src := strings.TrimPrefix(r.Header.Get("x-amz-copy-source"), "/")
		srcBucket, srcKey, _ := strings.Cut(src, "/")
		m.objects[bucket+"/"+key] = m.objects[srcBucket+"/"+srcKey]
		m.copied = append(m.copied, bucket+"/"+key)
		_, _ = io.WriteString(w, `<CopyObjectResult><ETag>"etag-new"</ETag></CopyObjectResult>`)

	case r.Method == http.MethodPut:
		n := 0
		if r.ContentLength > 0 {
			buf := make([]byte, r.ContentLength)
			n, _ = io.ReadFull(r.Body, buf)
			m.objects[bucket+"/"+key] = string(buf[:n])
		} else {
			m.objects[bucket+"/"+key] = ""
		}
		m.copied = append(m.copied, bucket+"/"+key)
		w.WriteHeader(http.StatusOK)

	case r.Method == http.MethodDelete:
		delete(m.objects, bucket+"/"+key)
		m.deleted = append(m.deleted, bucket+"/"+key)
		w.WriteHeader(http.StatusNoContent)

	case r.Method == http.MethodPost && q.Has("delete"):
		// 批量删除 (DeleteObjects): mirror 的删除阶段走这条路径, 而不是逐个 DELETE。
		raw, _ := io.ReadAll(r.Body)
		body := string(raw)
		for {
			i := strings.Index(body, "<Key>")
			if i < 0 {
				break
			}
			rest := body[i+len("<Key>"):]
			j := strings.Index(rest, "</Key>")
			if j < 0 {
				break
			}
			k := rest[:j]
			delete(m.objects, bucket+"/"+k)
			m.deleted = append(m.deleted, bucket+"/"+k)
			body = rest[j:]
		}
		_, _ = io.WriteString(w, `<DeleteResult></DeleteResult>`)

	default:
		w.WriteHeader(http.StatusBadRequest)
	}
}

func (m *mirrorServer) snapshot() (copied, deleted []string, objects map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := append([]string(nil), m.copied...)
	dl := append([]string(nil), m.deleted...)
	objs := make(map[string]string, len(m.objects))
	for k, v := range m.objects {
		objs[k] = v
	}
	return cp, dl, objs
}

func mirrorAction(t *testing.T, s *mirrorServer, alias string) *Action {
	t.Helper()
	builtin, err := api.New(&api.Options{
		Endpoint: s.srv.URL, AccessKey: "access", SecretKey: "secret",
		MaxRetries: 1, BucketLookup: api.BucketLookupPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &Action{S3: api.S3Operations(builtin), Alias: alias, Ctx: context.Background()}
}

func mirrorOpts(t *testing.T, s *mirrorServer) MirrorOptions {
	t.Helper()
	return MirrorOptions{
		Src:         &S3PathOptions{Client: mirrorAction(t, s, "src"), Bucket: "src"},
		Tgt:         &S3PathOptions{Client: mirrorAction(t, s, "tgt"), Bucket: "tgt"},
		Concurrency: 2,
		NoProgress:  true,
	}
}

// TestMirrorCopiesMissingObjects 覆盖主流程: 源独有的对象被复制到目标,
// 已存在且一致的对象不重复复制。
func TestMirrorCopiesMissingObjects(t *testing.T) {
	s := newMirrorServer(t, map[string]string{
		"src/a.txt": "alpha",
		"src/b.txt": "beta",
		"tgt/a.txt": "alpha", // 已存在且大小一致 -> 跳过
	})
	if err := Mirror(mirrorOpts(t, s)); err != nil {
		t.Fatal(err)
	}

	copied, deleted, objects := s.snapshot()
	if len(copied) != 1 || copied[0] != "tgt/b.txt" {
		t.Fatalf("copied = %v, want exactly [tgt/b.txt]", copied)
	}
	if len(deleted) != 0 {
		t.Fatalf("deleted = %v, want none (mirror without --remove must never delete)", deleted)
	}
	if objects["tgt/b.txt"] != "beta" {
		t.Fatalf("tgt/b.txt = %q, want %q", objects["tgt/b.txt"], "beta")
	}
}

// TestMirrorDryRunMakesNoChanges 干跑不得产生任何写操作, 也不得创建 manifest 文件。
func TestMirrorDryRunMakesNoChanges(t *testing.T) {
	s := newMirrorServer(t, map[string]string{
		"src/a.txt":   "alpha",
		"tgt/old.txt": "stale",
	})
	opt := mirrorOpts(t, s)
	opt.DryRun = true
	opt.Remove = true
	opt.ManifestPath = filepath.Join(t.TempDir(), "manifest.txt")

	if err := Mirror(opt); err != nil {
		t.Fatal(err)
	}

	copied, deleted, _ := s.snapshot()
	if len(copied) != 0 || len(deleted) != 0 {
		t.Fatalf("dry-run performed writes: copied=%v deleted=%v", copied, deleted)
	}
	if _, err := os.Stat(opt.ManifestPath); !os.IsNotExist(err) {
		t.Fatalf("dry-run must not create the manifest file (err=%v)", err)
	}
}

// TestMirrorRemoveDeletesExtraTargetObjects 覆盖 --remove 的正常删除路径。
func TestMirrorRemoveDeletesExtraTargetObjects(t *testing.T) {
	s := newMirrorServer(t, map[string]string{
		"src/a.txt":     "alpha",
		"tgt/a.txt":     "alpha",
		"tgt/extra.txt": "junk",
	})
	opt := mirrorOpts(t, s)
	opt.Remove = true
	if err := Mirror(opt); err != nil {
		t.Fatal(err)
	}

	_, deleted, objects := s.snapshot()
	if len(deleted) != 1 || deleted[0] != "tgt/extra.txt" {
		t.Fatalf("deleted = %v, want exactly [tgt/extra.txt]", deleted)
	}
	if _, still := objects["tgt/extra.txt"]; still {
		t.Fatal("extra object was not actually removed")
	}
	if objects["tgt/a.txt"] != "alpha" {
		t.Fatal("matching object must be preserved")
	}
}

// TestMirrorRemoveIsFencedWhenCopyFails 是数据安全回归测试 (mirror 的核心围栏)。
//
// 只要有一个对象复制失败, 就绝不能进入 --remove 的删除阶段: 那些"目标端多余"
// 的对象, 很可能正是这次复制失败、本该被覆盖的对象。若删除阶段照常执行,
// 一次网络抖动就会从目标端成批删掉本应存在的对象。
func TestMirrorRemoveIsFencedWhenCopyFails(t *testing.T) {
	s := newMirrorServer(t, map[string]string{
		// 源与目标大小不同 -> needsUpdate 必然为真, mirror 一定会尝试复制 b.txt。
		// (size 相同时它还会退化为比较 LastModified, 那样就构造不出"复制失败"。)
		"src/b.txt":     "beta-new-longer-content",
		"tgt/b.txt":     "beta-old",
		"tgt/other.txt": "should-survive",
	})
	opt := mirrorOpts(t, s)
	opt.Remove = true
	opt.Overwrite = true
	// 让 b.txt 的复制必定失败。
	s.mu.Lock()
	s.failCopy["tgt/b.txt"] = true
	s.mu.Unlock()

	err := Mirror(opt)
	if err == nil {
		t.Fatal("mirror must report the copy failure")
	}

	_, deleted, objects := s.snapshot()
	if len(deleted) != 0 {
		t.Fatalf("deleted = %v, want none: the delete phase must be fenced off when any copy failed", deleted)
	}
	if objects["tgt/other.txt"] != "should-survive" {
		t.Fatal("a failed copy must not lead to deleting unrelated target objects")
	}
	if objects["tgt/b.txt"] != "beta-old" {
		t.Fatal("the object whose copy failed must be left untouched")
	}
}

// TestMirrorMaxDeleteLimitsDeletions 覆盖 --max-delete 安全阀:
// 超出上限时必须拒绝删除而不是"删一部分"。
func TestMirrorMaxDeleteLimitsDeletions(t *testing.T) {
	s := newMirrorServer(t, map[string]string{
		"src/a.txt":  "alpha",
		"tgt/a.txt":  "alpha",
		"tgt/x1.txt": "junk",
		"tgt/x2.txt": "junk",
		"tgt/x3.txt": "junk",
	})
	opt := mirrorOpts(t, s)
	opt.Remove = true
	opt.MaxDelete = 2 // 有 3 个待删 -> 必须整体拒绝

	err := Mirror(opt)
	if err == nil {
		t.Fatal("mirror must refuse when pending deletions exceed --max-delete")
	}

	_, deleted, objects := s.snapshot()
	if len(deleted) != 0 {
		t.Fatalf("deleted = %v, want none: exceeding --max-delete must abort before deleting anything", deleted)
	}
	for _, k := range []string{"tgt/x1.txt", "tgt/x2.txt", "tgt/x3.txt"} {
		if _, ok := objects[k]; !ok {
			t.Fatalf("%s was deleted despite the --max-delete guard", k)
		}
	}
}

// TestMirrorIncludeExcludeFilters 覆盖 --include/--exclude 过滤。
func TestMirrorIncludeExcludeFilters(t *testing.T) {
	s := newMirrorServer(t, map[string]string{
		"src/keep/a.txt": "1",
		"src/skip/b.log": "2",
	})
	opt := mirrorOpts(t, s)
	opt.Exclude = []string{"*.log"}
	if err := Mirror(opt); err != nil {
		t.Fatal(err)
	}

	copied, _, objects := s.snapshot()
	if len(copied) != 1 || copied[0] != "tgt/keep/a.txt" {
		t.Fatalf("copied = %v, want exactly [tgt/keep/a.txt]", copied)
	}
	if _, ok := objects["tgt/skip/b.log"]; ok {
		t.Fatal("--exclude '*.log' must skip b.log")
	}
}

// TestMirrorResumeSkipsManifestEntries 覆盖 manifest 断点续传: 已记录的 key 被跳过。
func TestMirrorResumeSkipsManifestEntries(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "manifest.txt")
	if err := os.WriteFile(manifest, []byte("a.txt\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	s := newMirrorServer(t, map[string]string{
		"src/a.txt": "alpha",
		"src/b.txt": "beta",
	})
	opt := mirrorOpts(t, s)
	opt.ManifestPath = manifest
	opt.Resume = true

	if err := Mirror(opt); err != nil {
		t.Fatal(err)
	}

	copied, _, _ := s.snapshot()
	if len(copied) != 1 || copied[0] != "tgt/b.txt" {
		t.Fatalf("copied = %v, want exactly [tgt/b.txt] (a.txt is already in the manifest)", copied)
	}
	// 本次成功复制的 key 必须追加进 manifest, 供下次续传。
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "b.txt") {
		t.Fatalf("manifest was not updated: %q", data)
	}
}

// TestMirrorResumeRequiresManifestPathResolve resume 需要 manifest 路径。
func TestMirrorResumeWithoutManifestIsRejected(t *testing.T) {
	s := newMirrorServer(t, map[string]string{"src/a.txt": "alpha"})
	opt := mirrorOpts(t, s)
	opt.Resume = true // 未设置 ManifestPath

	if err := Mirror(opt); err == nil {
		t.Fatal("--resume without a manifest path must be rejected")
	}
	copied, _, _ := s.snapshot()
	if len(copied) != 0 {
		t.Fatalf("nothing should be copied, got %v", copied)
	}
}
