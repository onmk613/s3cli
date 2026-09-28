// copy_large_test.go 覆盖 cp/mv 对 >5GiB 源对象的处理: 单次 CopyObject 会返回
// EntityTooLarge, 此时必须自动改走 CreateMultipartUpload + UploadPartCopy,
// 而不是把错误直接抛给用户 (mirror 早已支持, cp/mv 此前会必然失败)。

package action

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"s3cli/internal/api"
)

// largeCopyServer 模拟: CopyObject 一律 EntityTooLarge, 分片复制路径可用。
type largeCopyServer struct {
	mu sync.Mutex

	copyAttempts   int
	uploadID       string
	parts          []string // 记录每次 UploadPartCopy 的 Range
	completed      bool
	aborted        bool
	createTagging  string
	createMetadata string
}

func (s *largeCopyServer) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		q := r.URL.Query()
		switch {
		case r.Method == http.MethodHead:
			w.Header().Set("Content-Length", fmt.Sprint(6*1024*1024*1024)) // 6 GiB
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("x-amz-meta-src", "yes")
			w.WriteHeader(http.StatusOK)

		case r.Method == http.MethodPut && q.Has("partNumber"):
			s.parts = append(s.parts, r.Header.Get("x-amz-copy-source-range"))
			_, _ = io.WriteString(w, fmt.Sprintf(
				`<CopyPartResult><ETag>"part%d"</ETag><LastModified>2026-01-01T00:00:00.000Z</LastModified></CopyPartResult>`,
				len(s.parts)))

		case r.Method == http.MethodPut && r.Header.Get("x-amz-copy-source") != "":
			s.copyAttempts++
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `<Error><Code>EntityTooLarge</Code><Message>proposed upload exceeds the maximum allowed object size</Message></Error>`)

		case r.Method == http.MethodPost && q.Has("uploads"):
			s.uploadID = "upload-large"
			s.createTagging = r.Header.Get("x-amz-tagging")
			s.createMetadata = r.Header.Get("x-amz-meta-src")
			_, _ = io.WriteString(w, `<InitiateMultipartUploadResult><Bucket>bucket</Bucket><Key>dst</Key><UploadId>upload-large</UploadId></InitiateMultipartUploadResult>`)

		case r.Method == http.MethodPost && q.Has("uploadId"):
			s.completed = true
			_, _ = io.WriteString(w, `<CompleteMultipartUploadResult><ETag>"done"</ETag></CompleteMultipartUploadResult>`)

		case r.Method == http.MethodDelete && q.Has("uploadId"):
			s.aborted = true
			w.WriteHeader(http.StatusNoContent)

		default:
			w.WriteHeader(http.StatusOK)
		}
	})
}

func TestCopyFallsBackToMultipartOnEntityTooLarge(t *testing.T) {
	state := &largeCopyServer{}
	server := httptest.NewServer(state.handler())
	defer server.Close()

	client := &Action{S3: actionTestClient(t, server.URL, nil), Alias: "test", Ctx: context.Background()}
	if err := client.copyObject(CopyOptions{}, "bucket", "src-big", "bucket", "dst-big"); err != nil {
		t.Fatalf("cp must transparently fall back to multipart copy: %v", err)
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	if state.copyAttempts != 1 {
		t.Errorf("want exactly 1 direct CopyObject attempt, got %d", state.copyAttempts)
	}
	if !state.completed {
		t.Error("multipart upload was never completed")
	}
	if state.aborted {
		t.Error("upload must not be aborted on the success path")
	}
	if len(state.parts) == 0 {
		t.Fatal("no UploadPartCopy requests were made")
	}
	// 每片都必须带 Range 头, 且首片从 0 开始。
	if !strings.HasPrefix(state.parts[0], "bytes=0-") {
		t.Errorf("first part range = %q, want bytes=0-...", state.parts[0])
	}
}

// TestCopyMultipartFallbackPreservesMetadata 断言分片回退路径把源对象的元数据
// 带到 CreateMultipartUpload 上 (分片复制的元数据只能在初始化阶段指定)。
func TestCopyMultipartFallbackPreservesMetadata(t *testing.T) {
	state := &largeCopyServer{}
	server := httptest.NewServer(state.handler())
	defer server.Close()

	client := &Action{S3: actionTestClient(t, server.URL, nil), Alias: "test", Ctx: context.Background()}
	if err := client.copyObject(CopyOptions{}, "bucket", "src-big", "bucket", "dst-big"); err != nil {
		t.Fatal(err)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.createMetadata != "yes" {
		t.Errorf("source metadata must be carried into CreateMultipartUpload, got %q", state.createMetadata)
	}
}

// TestCopyDoesNotFallBackOnOtherErrors 断言非 EntityTooLarge 的错误不会被
// 误判为"需要分片", 否则任何 403 都会升级成一次无意义的分片上传。
func TestCopyDoesNotFallBackOnOtherErrors(t *testing.T) {
	var multipartCreated bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Query().Has("uploads") {
			multipartCreated = true
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `<Error><Code>AccessDenied</Code><Message>Access Denied.</Message></Error>`)
	}))
	defer server.Close()

	client := &Action{S3: actionTestClient(t, server.URL, nil), Alias: "test", Ctx: context.Background()}
	err := client.copyObject(CopyOptions{}, "bucket", "src", "bucket", "dst")
	if err == nil {
		t.Fatal("expected AccessDenied to surface")
	}
	if multipartCreated {
		t.Error("AccessDenied must not trigger the multipart fallback")
	}
	var apiErr *api.ErrorResponse
	if !errors.As(err, &apiErr) {
		t.Fatalf("error chain must stay intact for exit-code mapping, got %v", err)
	}
	if apiErr.Code != "AccessDenied" {
		t.Errorf("code = %q, want AccessDenied", apiErr.Code)
	}
}
