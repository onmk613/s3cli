// flag_wiring_test.go 覆盖 cmd 层 "flag 解析 -> Options -> 实际 HTTP 请求"
// 的接线。action 层单测直接构造 Options, CLI 层的漏接线 (flag 声明了但没写进
// Options, 或写错字段) 只有从这里走一遍才能暴露; json_wiring_test 已锁定
// --json 一类, 这里覆盖传输命令的核心 flag。
package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"s3cli/internal/config"
)

type recordedRequest struct {
	Method string
	Path   string
	Query  string
	Header http.Header
	Body   string
}

// recordingS3Server 记录所有进入的请求, 按需应答最小 S3 语义。
type recordingS3Server struct {
	srv *httptest.Server
	mu  sync.Mutex
	req []recordedRequest
}

func newRecordingS3Server(t *testing.T) *recordingS3Server {
	t.Helper()
	s := &recordingS3Server{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 4096)
		n, _ := r.Body.Read(body)
		s.mu.Lock()
		s.req = append(s.req, recordedRequest{
			Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery,
			Header: r.Header.Clone(), Body: string(body[:n]),
		})
		s.mu.Unlock()
		// 默认应答: put 的目标对象不存在 (触发真实上传), 其余对象 HEAD/GET
		// 命中; 列举按前缀返回一条对象; 服务端复制返回 CopyObjectResult;
		// 批删返回 DeleteResult; 其余 200。
		q := r.URL.Query()
		switch {
		case r.Method == http.MethodHead && strings.HasSuffix(r.URL.Path, "f.txt"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodHead:
			w.Header().Set("Content-Length", "4")
			w.WriteHeader(http.StatusOK)
		case q.Get("list-type") == "2":
			if q.Get("prefix") == "dir/" {
				_, _ = w.Write([]byte(`<ListBucketResult><IsTruncated>false</IsTruncated>` +
					`<Contents><Key>dir/x.txt</Key><Size>4</Size></Contents></ListBucketResult>`))
			} else {
				_, _ = w.Write([]byte(`<ListBucketResult><IsTruncated>false</IsTruncated></ListBucketResult>`))
			}
		case r.Header.Get("x-amz-copy-source") != "":
			_, _ = w.Write([]byte(`<CopyObjectResult><ETag>"e"</ETag></CopyObjectResult>`))
		case r.Method == http.MethodPost && q.Has("delete"):
			_, _ = w.Write([]byte(`<DeleteResult></DeleteResult>`))
		case r.Method == http.MethodGet:
			w.Header().Set("Content-Length", "4")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("data"))
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *recordingS3Server) requests() []recordedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]recordedRequest(nil), s.req...)
}

// withAliasEnv 安装指向 mock 服务端的别名配置 (每用例独立别名, 避开
// client.S3Clients 的别名级缓存), 结束后还原。
func withAliasEnv(t *testing.T, alias, endpoint string) {
	t.Helper()
	oldS, oldC := config.G.S, config.G.C
	t.Cleanup(func() { config.G.S, config.G.C = oldS, oldC })
	config.G.S = map[string]config.Static{
		alias: {HostBase: endpoint, AccessKey: "access", SecretKey: "secret", MaxRetries: 1},
	}
	config.G.C = "<test>"
}

// runCmd 以 cobra 正规流程解析 flag 并执行 (flags 由 cobra 消化, RunE 只收
// 位置参数 —— 与真实 CLI 完全同路)。
func runCmd(t *testing.T, c interface {
	Execute() error
	SetArgs([]string)
	SetContext(ctx context.Context)
}, args []string) error {
	t.Helper()
	c.SetArgs(args)
	c.SetContext(context.Background())
	return c.Execute()
}

// TestPutFlagsReachRequest: put 的元数据类 flag 必须逐个落到 PUT 请求上。
func TestPutFlagsReachRequest(t *testing.T) {
	server := newRecordingS3Server(t)
	withAliasEnv(t, "wiringput", server.srv.URL)

	local := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(local, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := runCmd(t, NewPutCmd(), []string{
		local, "wiringput:bkt/f.txt",
		"--content-type", "text/x-custom",
		"--storage-class", "GLACIER",
		"--tags", "team=dev",
		"--metadata", "owner=me",
		"--checksum", "crc32",
	})
	if err != nil {
		t.Fatalf("put: %v", err)
	}

	var put *recordedRequest
	reqs := server.requests()
	for i, r := range reqs {
		if r.Method == http.MethodPut {
			put = &reqs[i]
			break
		}
	}
	if put == nil {
		for _, r := range reqs {
			t.Logf("saw %s %s?q=%s hdrs=%v", r.Method, r.Path, r.Query, r.Header)
		}
		t.Fatal("no PUT request reached the server")
	}
	assertHeader(t, put, "Content-Type", "text/x-custom")
	assertHeader(t, put, "X-Amz-Storage-Class", "GLACIER")
	assertHeader(t, put, "X-Amz-Tagging", "team=dev")
	assertHeader(t, put, "X-Amz-Meta-Owner", "me")
	assertHeader(t, put, "X-Amz-Sdk-Checksum-Algorithm", "CRC32")
}

// TestPutDryRunSendsNothing: --dry-run 不得发出任何请求。
func TestPutDryRunSendsNothing(t *testing.T) {
	server := newRecordingS3Server(t)
	withAliasEnv(t, "wiringdry", server.srv.URL)

	local := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(local, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, NewPutCmd(), []string{local, "wiringdry:bkt/f.txt", "--dry-run"}); err != nil {
		t.Fatalf("put --dry-run: %v", err)
	}
	if n := len(server.requests()); n != 0 {
		t.Fatalf("--dry-run sent %d HTTP requests, want 0", n)
	}
}

// TestGetFlagsReachRequest: get 的 --range / --version-id 必须落到 GET 上。
func TestGetFlagsReachRequest(t *testing.T) {
	server := newRecordingS3Server(t)
	withAliasEnv(t, "wiringget", server.srv.URL)

	out := filepath.Join(t.TempDir(), "out.bin")
	// --range 与 --version-id 互斥 (action 层校验), 分两次各验一条。
	if err := runCmd(t, NewGetCmd(), []string{"wiringget:bkt/g.txt", out, "--range", "bytes=0-3"}); err != nil {
		t.Fatalf("get --range: %v", err)
	}
	out2 := filepath.Join(t.TempDir(), "out2.bin")
	if err := runCmd(t, NewGetCmd(), []string{"wiringget:bkt/g.txt", out2, "--version-id", "v-123"}); err != nil {
		t.Fatalf("get --version-id: %v", err)
	}

	reqs := server.requests()
	var withRange, withVersion *recordedRequest
	for i, r := range reqs {
		if r.Method != http.MethodGet {
			continue
		}
		if r.Header.Get("Range") == "bytes=0-3" {
			withRange = &reqs[i]
		}
		if containsParam(r.Query, "versionId") {
			withVersion = &reqs[i]
		}
	}
	if withRange == nil {
		t.Fatal("--range did not reach the GET request")
	}
	if withVersion == nil || !containsParam(withVersion.Query, "versionId=v-123") {
		t.Fatal("--version-id did not reach the GET query")
	}
}

// TestRmRecursiveFences: rm -r 必须先列举再批量删除, 且不带 --force 直接拒绝。
func TestRmRecursiveFences(t *testing.T) {
	server := newRecordingS3Server(t)
	withAliasEnv(t, "wiringrm", server.srv.URL)

	// 不带 --force: 递归删除被拒绝, 服务端只可能收到列举请求。
	if err := runCmd(t, NewRmCmd(), []string{"wiringrm:bkt/dir/", "-r"}); err == nil {
		t.Fatal("rm -r without --force must be rejected")
	}
	for _, r := range server.requests() {
		if r.Method == http.MethodDelete || r.Method == http.MethodPost {
			t.Fatalf("unforced recursive rm must not delete, saw %s %s", r.Method, r.Path)
		}
	}

	// 带 --force: 走通 (列举 + 批删)。
	if err := runCmd(t, NewRmCmd(), []string{"wiringrm:bkt/dir/", "-r", "--force"}); err != nil {
		t.Fatalf("rm -r --force: %v", err)
	}
	sawDelete := false
	for _, r := range server.requests() {
		if r.Method == http.MethodPost && containsParam(r.Query, "delete") {
			sawDelete = true
		}
	}
	if !sawDelete {
		t.Fatal("rm -r --force should issue a batch delete (POST ?delete)")
	}
}

// TestCpUsesServerSideCopy: 同端 cp 必须走服务端 CopyObject (零下载)。
func TestCpUsesServerSideCopy(t *testing.T) {
	server := newRecordingS3Server(t)
	withAliasEnv(t, "wiringcp", server.srv.URL)

	if err := runCmd(t, NewCpCmd(), []string{"wiringcp:bkt/a.txt", "wiringcp:bkt/b.txt"}); err != nil {
		t.Fatalf("cp: %v", err)
	}
	for _, r := range server.requests() {
		if r.Method == http.MethodPut {
			assertHeader(t, &r, "X-Amz-Copy-Source", "/bkt/a.txt")
			return
		}
	}
	t.Fatal("cp did not issue a CopyObject PUT")
}

func assertHeader(t *testing.T, r *recordedRequest, key, want string) {
	t.Helper()
	if got := r.Header.Get(key); got != want {
		t.Fatalf("%s %s: header %s = %q, want %q", r.Method, r.Path, key, got, want)
	}
}

func containsParam(query, param string) bool {
	for _, kv := range splitAmp(query) {
		if kv == param || (len(kv) > len(param) && kv[:len(param)+1] == param+"=") {
			return true
		}
	}
	return false
}

func splitAmp(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '&' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}
