// mirror_range_test.go 锁定跨端分片复制的 Range 响应校验语义。
//
// 背景: 旧实现发出 bytes=offset-end 后直接 io.ReadAll, 不校验 Content-Range
// 与长度 —— 个别网关会忽略 Range 头返回 200 全量对象, 分片错位后最终对象
// 静默损坏。这里用"忽略 Range 的服务端"复现该场景, 断言复制失败且 Abort 被调用。
package action

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// rangeSourceServer 是一个正确支持 Range 的源端: 按 bytes=start-end 截取内容,
// 返回 206 + Content-Range。ignoreRange=true 时模拟病态网关: 忽略 Range 头,
// 直接 200 返回全量 (无 Content-Range)。
type rangeSourceServer struct {
	srv         *httptest.Server
	content     string
	ignoreRange bool
}

func newRangeSourceServer(t *testing.T, content string, ignoreRange bool) *rangeSourceServer {
	t.Helper()
	s := &rangeSourceServer{content: content, ignoreRange: ignoreRange}
	s.srv = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *rangeSourceServer) URL() string { return s.srv.URL }

func (s *rangeSourceServer) handle(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodHead:
		w.Header().Set("Content-Length", fmt.Sprint(len(s.content)))
		w.WriteHeader(http.StatusOK)
	case http.MethodGet:
		if s.ignoreRange || r.Header.Get("Range") == "" {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, s.content)
			return
		}
		var start, end int
		if _, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if end >= len(s.content) {
			end = len(s.content) - 1
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(s.content)))
		w.Header().Set("Content-Length", fmt.Sprint(end-start+1))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = io.WriteString(w, s.content[start:end+1])
	default:
		w.WriteHeader(http.StatusInternalServerError)
	}
}

// targetUploadServer 记录分片上传服务端看到的每个分片字节, 并在 Complete 时
// 按分片号拼装出最终对象。
type targetUploadServer struct {
	srv      *httptest.Server
	mu       sync.Mutex
	parts    map[int][]byte
	aborted  bool
	complete []int
}

func newTargetUploadServer(t *testing.T) *targetUploadServer {
	t.Helper()
	s := &targetUploadServer{parts: map[int][]byte{}}
	s.srv = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *targetUploadServer) URL() string { return s.srv.URL }

func (s *targetUploadServer) handle(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	switch {
	case r.Method == http.MethodPost && q.Has("uploads"):
		_, _ = io.WriteString(w, `<InitiateMultipartUploadResult><UploadId>uid-range</UploadId></InitiateMultipartUploadResult>`)
	case r.Method == http.MethodPut && q.Has("partNumber"):
		body, _ := io.ReadAll(r.Body)
		num, _ := strconv.Atoi(q.Get("partNumber"))
		s.mu.Lock()
		s.parts[num] = append([]byte(nil), body...)
		s.mu.Unlock()
		_, _ = io.WriteString(w, `<empty/>`)
	case r.Method == http.MethodPost && q.Has("uploadId"):
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		for _, seg := range strings.Split(string(body), "<PartNumber>") {
			if idx := strings.Index(seg, "</PartNumber>"); idx > 0 {
				if n, err := strconv.Atoi(seg[:idx]); err == nil {
					s.complete = append(s.complete, n)
				}
			}
		}
		s.mu.Unlock()
		_, _ = io.WriteString(w, `<CompleteMultipartUploadResult><ETag>"final"</ETag></CompleteMultipartUploadResult>`)
	case r.Method == http.MethodDelete && q.Has("uploadId"):
		s.mu.Lock()
		s.aborted = true
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusBadRequest)
	}
}

// assembled 返回按分片号升序拼装的目标对象内容。
func (s *targetUploadServer) assembled() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var b strings.Builder
	for i := 1; ; i++ {
		part, ok := s.parts[i]
		if !ok {
			return b.String()
		}
		b.Write(part)
	}
}

// TestCrossEndpointMultipartCopyAssemblesExactParts: 正确的 Range 服务端 +
// 分片并行复制, 目标对象必须与源字节一致, Complete 按升序提交。
func TestCrossEndpointMultipartCopyAssemblesExactParts(t *testing.T) {
	// 12MB, partSize 5MB -> 3 片 (5MB + 5MB + 2MB)。
	content := strings.Repeat("0123456789abcdef", 12*64*1024)
	srcSrv := newRangeSourceServer(t, content, false)
	tgtSrv := newTargetUploadServer(t)

	src := &Action{S3: actionTestClient(t, srcSrv.URL(), nil), Ctx: context.Background()}
	tgt := &Action{S3: actionTestClient(t, tgtSrv.URL(), nil), Ctx: context.Background()}

	if err := copyObjectCrossEndpoint(src, tgt, "sb", "big.bin", "tb", "big.bin", "", 5*1024*1024, nil); err != nil {
		t.Fatalf("cross-endpoint multipart copy failed: %v", err)
	}
	if got := tgtSrv.assembled(); got != content {
		t.Fatalf("assembled object corrupted: got %d bytes, want %d", len(got), len(content))
	}
	tgtSrv.mu.Lock()
	defer tgtSrv.mu.Unlock()
	if len(tgtSrv.complete) != 3 {
		t.Fatalf("complete listed %d parts, want 3", len(tgtSrv.complete))
	}
	for i, n := range tgtSrv.complete {
		if n != i+1 {
			t.Fatalf("complete part order = %v, want ascending [1 2 3]", tgtSrv.complete)
		}
	}
	if tgtSrv.aborted {
		t.Fatal("abort must not be called on success")
	}
}

// TestCrossEndpointCopyRejectsRangeIgnoringGateway: 网关忽略 Range 返回
// 200 全量时, 复制必须失败 (而不是把全量字节当分片拼出坏对象), 并 Abort。
func TestCrossEndpointCopyRejectsRangeIgnoringGateway(t *testing.T) {
	content := strings.Repeat("x", 12*1024*1024)
	srcSrv := newRangeSourceServer(t, content, true)
	tgtSrv := newTargetUploadServer(t)

	src := &Action{S3: actionTestClient(t, srcSrv.URL(), nil), Ctx: context.Background()}
	tgt := &Action{S3: actionTestClient(t, tgtSrv.URL(), nil), Ctx: context.Background()}

	err := copyObjectCrossEndpoint(src, tgt, "sb", "big.bin", "tb", "big.bin", "", 5*1024*1024, nil)
	if err == nil {
		t.Fatal("copy must fail when the gateway ignores Range requests")
	}
	if !strings.Contains(err.Error(), "Range") && !strings.Contains(err.Error(), "range") {
		t.Fatalf("error should attribute the failure to the Range response, got: %v", err)
	}
	tgtSrv.mu.Lock()
	defer tgtSrv.mu.Unlock()
	if !tgtSrv.aborted {
		t.Fatal("AbortMultipartUpload must be issued after the fenced failure")
	}
}

// TestCrossEndpointSingleCopyDetectsTruncation: 小对象路径 (单次 PUT) 的
// 下载长度与 HEAD 不符时必须失败, 不能上传半个对象。
func TestCrossEndpointSingleCopyDetectsTruncation(t *testing.T) {
	const headSize = 1024
	const served = 512 // HEAD 声明 1024, GET 只给 512
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodHead:
			w.Header().Set("Content-Length", fmt.Sprint(headSize))
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			w.Header().Set("Content-Length", fmt.Sprint(served))
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, strings.Repeat("y", served))
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	src := &Action{S3: actionTestClient(t, srv.URL, nil), Ctx: context.Background()}
	tgt := &Action{S3: actionTestClient(t, srv.URL, nil), Ctx: context.Background()}
	err := copyObjectCrossEndpoint(src, tgt, "sb", "small.bin", "tb", "small.bin", "", 5*1024*1024, nil)
	if err == nil {
		t.Fatal("copy must fail on truncated download")
	}
	if !strings.Contains(err.Error(), "expected") {
		t.Fatalf("error should report the length mismatch, got: %v", err)
	}
}
