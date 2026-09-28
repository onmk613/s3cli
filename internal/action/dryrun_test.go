package action

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// countRequests 起一个只统计请求数的服务端, 用于断言"干跑不发任何请求"。
func countRequests(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var n atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)
	return server, &n
}

// TestPutDryRunIssuesNoRequests 是回归测试。
//
// 修复前: --dry-run 只在目录分支 (uploadDirStreaming 的 Work 闭包) 被检查,
// 单文件与 stdin 分支照常上传 —— 用户以为在预演, 实际覆盖了线上对象。
// 干跑必须零请求: 连存在性探测都不发 (与目录分支语义一致)。
func TestPutDryRunIssuesNoRequests(t *testing.T) {
	server, requests := countRequests(t)
	newAction := func() *Action {
		return &Action{S3: actionTestClient(t, server.URL, nil), Ctx: context.Background()}
	}

	dir := t.TempDir()
	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("single-file", func(t *testing.T) {
		if err := newAction().PutObject(PutOptions{DryRun: true}, "bucket", "f.txt", file, false); err != nil {
			t.Fatalf("dry-run returned error: %v", err)
		}
	})

	t.Run("stdin", func(t *testing.T) {
		// putStdin 读 os.Stdin; 干跑必须在读取之前短路, 因此这里给一个会
		// 阻塞/报错的 reader 也不该被碰到 —— 用 /dev/null 足够, 若实现回归
		// 到"先读后判", 本用例仍会因请求计数而失败。
		if err := newAction().PutObject(PutOptions{DryRun: true}, "bucket", "k.txt", "-", false); err != nil {
			t.Fatalf("dry-run returned error: %v", err)
		}
	})

	t.Run("directory", func(t *testing.T) {
		sub := filepath.Join(dir, "sub")
		if err := os.MkdirAll(sub, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sub, "g.txt"), []byte("world"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := newAction().PutObject(PutOptions{DryRun: true, Recursive: true}, "bucket", "dest", sub, false); err != nil {
			t.Fatalf("dry-run returned error: %v", err)
		}
	})

	if n := requests.Load(); n != 0 {
		t.Fatalf("put --dry-run issued %d HTTP request(s); want 0 (dry-run must not touch the network)", n)
	}
}

// TestPutDryRunDoesNotConsumeStdin 断言干跑不会读取 stdin:
// 管道数据必须留给调用方, 否则 `producer | s3cli put --dry-run - k` 会静默吃掉输入。
func TestPutDryRunDoesNotConsumeStdin(t *testing.T) {
	server, _ := countRequests(t)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString("payload-that-must-not-be-consumed"); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()

	oldStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = oldStdin; _ = r.Close() })

	a := &Action{S3: actionTestClient(t, server.URL, nil), Ctx: context.Background()}
	if err := a.PutObject(PutOptions{DryRun: true}, "bucket", "k.txt", "-", false); err != nil {
		t.Fatalf("dry-run returned error: %v", err)
	}

	// 数据仍可读 => 没被消费。
	buf := make([]byte, 64)
	n, _ := r.Read(buf)
	if n == 0 {
		t.Fatal("dry-run consumed stdin; piped data was discarded")
	}
}
