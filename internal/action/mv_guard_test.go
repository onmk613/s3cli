package action

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"s3cli/internal/s3path"
)

// mvGuardServer 让 key "dir/f.txt" 看起来是一个已存在的对象 (HEAD 命中),
// 并记录所有写操作。用于验证 mv 的自目标守卫。
func mvGuardServer(t *testing.T, copies, deletes *atomic.Int32, deletesKeys *sync.Map) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case r.Method == http.MethodHead && strings.HasSuffix(r.URL.Path, "/dir/f.txt"):
			w.Header().Set("Content-Length", "5")
			w.Header().Set("ETag", `"e"`)
			w.Header().Set("Last-Modified", "Wed, 21 Oct 2026 07:28:00 GMT")
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodHead:
			httpError(w, http.StatusNotFound, "NoSuchKey", "not found")
		case q.Get("list-type") == "2":
			_, _ = io.WriteString(w, `<ListBucketResult><IsTruncated>false</IsTruncated></ListBucketResult>`)
		case r.Method == http.MethodPut && r.Header.Get("x-amz-copy-source") != "":
			copies.Add(1)
			_, _ = io.WriteString(w, `<CopyObjectResult><ETag>"e"</ETag></CopyObjectResult>`)
		case r.Method == http.MethodDelete:
			deletes.Add(1)
			deletesKeys.Store(r.URL.Path, true)
			w.WriteHeader(http.StatusNoContent)
		default:
			httpError(w, http.StatusBadRequest, "InvalidRequest", "unsupported")
		}
	}))
}

// TestMvSelfDestinationIsRefused 是回归测试。
//
// 修复前 `mv a:b/dir/f.txt a:b/dir/` 与 `mv a:b/k a:b` 都会把目标解析回源
// 对象: 自拷贝在 S3 上是合法请求 (不会失败), 紧随其后的删除于是把对象本身
// 抹掉 —— 一次手滑即永久丢数据。
func TestMvSelfDestinationIsRefused(t *testing.T) {
	var copies, deletes atomic.Int32
	var deletedKeys sync.Map
	server := mvGuardServer(t, &copies, &deletes, &deletedKeys)
	defer server.Close()

	newAction := func() *Action {
		return &Action{S3: actionTestClient(t, server.URL, nil), Alias: "test", Ctx: context.Background()}
	}

	cases := []struct {
		name            string
		srcKey, destKey string
	}{
		{"file-into-own-dir", "dir/f.txt", "dir/"},
		{"explicit-identical-key", "dir/f.txt", "dir/f.txt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := newAction().Mv(CopyOptions{NoProgress: true}, "bucket", tc.srcKey, "bucket", tc.destKey)
			if err == nil {
				t.Fatalf("mv %q -> %q must be refused (it would delete the source)", tc.srcKey, tc.destKey)
			}
			if !strings.Contains(err.Error(), "同一个对象") && !strings.Contains(err.Error(), "same object") &&
				!strings.Contains(err.Error(), "same:") {
				t.Fatalf("unexpected error message: %v", err)
			}
		})
	}

	// 干跑同样必须报错: 修复前它会打印误导性的 "X -> X" 计划行。
	t.Run("dry-run-also-refuses", func(t *testing.T) {
		if err := newAction().Mv(CopyOptions{DryRun: true, NoProgress: true}, "bucket", "dir/f.txt", "bucket", "dir/"); err == nil {
			t.Fatal("dry-run must report the same-object problem instead of printing X -> X")
		}
	})

	if copies.Load() != 0 || deletes.Load() != 0 {
		t.Fatalf("refused self-move still issued %d copy / %d delete request(s); want 0", copies.Load(), deletes.Load())
	}
}

// TestMvLegitimateMoveStillWorks 确保守卫没有把正常移动一并挡掉。
func TestMvLegitimateMoveStillWorks(t *testing.T) {
	var copies, deletes atomic.Int32
	var deletedKeys sync.Map
	server := mvGuardServer(t, &copies, &deletes, &deletedKeys)
	defer server.Close()

	a := &Action{S3: actionTestClient(t, server.URL, nil), Alias: "test", Ctx: context.Background()}
	if err := a.Mv(CopyOptions{NoProgress: true}, "bucket", "dir/f.txt", "bucket", "other/f.txt"); err != nil {
		t.Fatalf("legitimate move failed: %v", err)
	}
	if copies.Load() != 1 {
		t.Fatalf("copies = %d, want 1", copies.Load())
	}
	if deletes.Load() != 1 {
		t.Fatalf("deletes = %d, want 1 (source must be removed after a real move)", deletes.Load())
	}
	if _, removed := deletedKeys.Load("/bucket/dir/f.txt"); !removed {
		t.Fatal("the deleted key was not the source object")
	}
}

// TestMvWithoutTrailingSlashIsARename 锁定"尾斜杠 = 目录"的约定:
// `mv a:b/dir/f.txt a:b/dir` 的目标是无尾斜杠的完整 key, 语义是把对象改名成
// "dir", 属于合法移动 —— 守卫不能把它误判成自移动而拒绝。
func TestMvWithoutTrailingSlashIsARename(t *testing.T) {
	var copies, deletes atomic.Int32
	var deletedKeys sync.Map
	server := mvGuardServer(t, &copies, &deletes, &deletedKeys)
	defer server.Close()

	a := &Action{S3: actionTestClient(t, server.URL, nil), Alias: "test", Ctx: context.Background()}
	if err := a.Mv(CopyOptions{NoProgress: true}, "bucket", "dir/f.txt", "bucket", "renamed"); err != nil {
		t.Fatalf("rename must stay allowed: %v", err)
	}
	if copies.Load() != 1 || deletes.Load() != 1 {
		t.Fatalf("copies=%d deletes=%d, want 1/1", copies.Load(), deletes.Load())
	}
}

// TestCheckSameObject 覆盖 key 归一化边界: 只吃尾部 "/", 前导 "/" 必须保留
// (S3 key 的前导斜杠是有意义的差异, 抹平会误判成同一个对象)。
func TestCheckSameObject(t *testing.T) {
	cases := []struct {
		name                   string
		srcB, srcK, dstB, dstK string
		same                   bool
	}{
		{"identical", "b", "k", "b", "k", true},
		{"trailing-slash-only-diff", "b", "dir/", "b", "dir", true},
		{"different-key", "b", "a", "b", "b", false},
		{"different-bucket", "b1", "k", "b2", "k", false},
		{"leading-slash-is-meaningful", "b", "/k", "b", "k", false},
		{"case-sensitive", "b", "K", "b", "k", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkSameObject(tc.srcB, tc.srcK, tc.dstB, tc.dstK)
			if got := err != nil; got != tc.same {
				t.Fatalf("checkSameObject(%q,%q,%q,%q) same=%v, want %v (err=%v)",
					tc.srcB, tc.srcK, tc.dstB, tc.dstK, got, tc.same, err)
			}
		})
	}
}

// 确保测试用的路径解析前提仍然成立 (ResolveFileDest 会把目录还原成源 basename)。
func TestResolveFileDestPreconditionForMvGuard(t *testing.T) {
	if got := s3path.ResolveFileDest("dir/", true, "f.txt"); got != "dir/f.txt" {
		t.Fatalf("ResolveFileDest = %q, want dir/f.txt", got)
	}
	if got := s3path.ResolveFileDest("", false, "k"); got != "k" {
		t.Fatalf("ResolveFileDest = %q, want k", got)
	}
}
