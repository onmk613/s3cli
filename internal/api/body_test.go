// body_test.go 覆盖 Do 返回响应体的两项统一保护:
// 停滞检测 (responseBody.Read) 与排空关闭 (连接复用)。
package api

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// TestResponseBodyStallDetection: 服务端发完头部与少量字节后挂起,
// 停滞保护必须在 StallTimeout 内让 Read 失败, 而不是永久阻塞。
func TestResponseBodyStallDetection(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1024")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(make([]byte, 16)) // 先给一点数据
		// 显式 Flush: 否则这 16 字节停留在服务端 bufio 缓冲里, 客户端
		// 连响应头都收不到 (卡在 ResponseHeaderTimeout 而非本测试的停滞路径)。
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-release // 然后永久挂起
	}))
	defer srv.Close()
	defer close(release)

	c, err := New(&Options{Endpoint: srv.URL, AccessKey: "a", SecretKey: "b", StallTimeout: 150 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	out, err := c.GetObject(context.Background(), "bucket", "key", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = out.Body.Close() }()

	buf := make([]byte, 16)
	if _, err := io.ReadFull(out.Body, buf); err != nil {
		t.Fatalf("first read should succeed: %v", err)
	}

	start := time.Now()
	_, err = out.Body.Read(make([]byte, 16))
	if err == nil {
		t.Fatal("read after stall should fail")
	}
	if !errors.Is(err, errBodyStalled) {
		t.Fatalf("error should be errBodyStalled, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("stall detected too late: %v", elapsed)
	}
}

// TestResponseBodyStallDisabled: StallTimeout < 0 时禁用停滞保护, 慢而持续
// 的响应体仍能正常读完。
func TestResponseBodyStallDisabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "32")
		w.WriteHeader(http.StatusOK)
		for i := 0; i < 32; i++ {
			_, _ = w.Write([]byte{'x'})
			time.Sleep(20 * time.Millisecond) // 慢于默认 60s 不会触发, 此处验证不干扰
		}
	}))
	defer srv.Close()

	c, err := New(&Options{Endpoint: srv.URL, AccessKey: "a", SecretKey: "b", StallTimeout: -1})
	if err != nil {
		t.Fatal(err)
	}
	out, err := c.GetObject(context.Background(), "bucket", "key", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = out.Body.Close() }()

	data, err := io.ReadAll(out.Body)
	if err != nil {
		t.Fatalf("slow body should read to EOF with stall protection disabled: %v", err)
	}
	if len(data) != 32 {
		t.Fatalf("read %d bytes, want 32", len(data))
	}
}

// TestXMLResponseConnectionReuse: XML 响应在 Decode 后由 Close 排空到 EOF,
// 连接归还连接池 —— 连续多个请求应复用同一条 TCP 连接。
// (旧实现 Close 前不排空, 每个请求都重新建连, MaxIdleConnsPerHost 形同虚设。)
func TestXMLResponseConnectionReuse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var conns atomic.Int32
	counting := &countingListener{Listener: ln, accepted: &conns}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 结尾带换行: Decode 读到根元素闭合即返回, 残留字节靠 Close 排空。
		_, _ = w.Write([]byte("<ListMultipartUploadsResult><Bucket>b</Bucket></ListMultipartUploadsResult>\n"))
	})}
	go func() { _ = srv.Serve(counting) }()
	t.Cleanup(func() { _ = srv.Close() })

	c, err := New(&Options{
		Endpoint:  "http://" + counting.Addr().String(),
		AccessKey: "a", SecretKey: "b",
	})
	if err != nil {
		t.Fatal(err)
	}

	const calls = 3
	for i := 0; i < calls; i++ {
		if _, err := c.ListMultipartUploads(context.Background(), "bucket", nil); err != nil {
			t.Fatalf("call %d failed: %v", i+1, err)
		}
	}
	// 等待连接归还池的微小延迟后发起下一轮已由循环覆盖; 直接断言建连数。
	if got := conns.Load(); got != 1 {
		t.Fatalf("opened %d TCP connections for %d sequential XML requests, want 1 (connection reuse broken)", got, calls)
	}
}

type countingListener struct {
	net.Listener
	accepted *atomic.Int32
}

func (l *countingListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err == nil {
		l.accepted.Add(1)
	}
	return conn, err
}

// TestUploadPartReaderStreamsFromSeeker 覆盖新增的流式分片上传:
// 从 *os.File 读取分片, 服务端收到的字节必须与文件一致。
func TestUploadPartReaderStreamsFromSeeker(t *testing.T) {
	var got []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Method == http.MethodPut && r.URL.Query().Has("partNumber") {
			got = body
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, err := New(&Options{Endpoint: srv.URL, AccessKey: "a", SecretKey: "b"})
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "part")
	if err := os.WriteFile(path, []byte("part-content-0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	content := "part-content-0123456789"
	out, err := c.UploadPartReader(context.Background(), "bucket", "key", "uid", 1, f, int64(len(content)), "")
	if err != nil {
		t.Fatalf("UploadPartReader: %v", err)
	}
	_ = out
	if string(got) != content {
		t.Fatalf("server received %q, want file content", got)
	}
}
