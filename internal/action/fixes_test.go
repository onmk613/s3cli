// fixes_test.go — 针对本轮修复的回归测试:
//  1. mirror 删除失败不再被吞 (copyAndDelete 返回删除错误)
//  2. 跨端分片复制失败/取消时用 WithoutCancel 清理服务端分片上传
//  3. MPU 服务端列举翻页 (MpuList / MpuAbort / findUploadKey / rm -I)
//  4. stat 用量统计不再静默吞掉列举错误
//  5. put 目录扫描感知取消; RunStream 返回前 join 全部扫描协程
//  6. cp/mv 目录复制跳过 0 字节目录占位对象
//  7. 断点续传 NoSuchUpload 自愈 + 其它 ListParts 错误提示 local-clear
//  8. rm --include/--exclude 按相对参数前缀的相对 key 匹配 (与 mirror 一致)
//  9. checkIfDirectory 不再把同名前缀文件误判为目录

package action

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"s3cli/pkg/s3iface"
)

// =============== Fix 1: mirror 删除失败传播 ===============

// TestMirrorDeleteFailurePropagates 验证 --remove 的批量删除失败时,
// copyAndDelete 仍打印汇总行与红字错误, 但最终返回错误 (不再静默成功)。
func TestMirrorDeleteFailurePropagates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("delete") {
			httpError(w, http.StatusInternalServerError, "InternalError", "delete failed")
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	api := actionTestClient(t, server.URL, nil)
	client := &Action{S3: api, Alias: "test", Ctx: context.Background()}
	plan := &mirrorPlan{cfg: MirrorOptions{Remove: true, NoProgress: true, Concurrency: 1}, srcClient: client, tgtClient: client, srcBucket: "source", tgtBucket: "target"}
	actions := make(chan diffAction, 1)
	actions <- diffAction{rel: "extra", delete: true}
	close(actions)

	var err error
	out := captureStdout(t, func() {
		err = plan.copyAndDelete(actions, make(chan error), nil)
	})
	if err == nil {
		t.Fatal("delete failure must be propagated as error")
	}
	if !strings.Contains(err.Error(), "delete") {
		t.Fatalf("error should mention delete, got: %v", err)
	}
	// 汇总行与红字错误仍需打印 (保持既有输出格式)
	if !strings.Contains(out, "Mirror done in") {
		t.Fatalf("summary line missing, output: %q", out)
	}
	if !strings.Contains(out, "delete error") {
		t.Fatalf("red delete-error line missing, output: %q", out)
	}
}

// TestMirrorDeleteSuccess 验证删除成功路径仍返回 nil。
func TestMirrorDeleteSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("delete") {
			_, _ = io.WriteString(w, `<DeleteResult></DeleteResult>`)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	api := actionTestClient(t, server.URL, nil)
	client := &Action{S3: api, Alias: "test", Ctx: context.Background()}
	plan := &mirrorPlan{cfg: MirrorOptions{Remove: true, NoProgress: true, Concurrency: 1}, srcClient: client, tgtClient: client, srcBucket: "source", tgtBucket: "target"}
	actions := make(chan diffAction, 1)
	actions <- diffAction{rel: "extra", delete: true}
	close(actions)
	var err error
	out := captureStdout(t, func() {
		err = plan.copyAndDelete(actions, make(chan error), nil)
	})
	if err != nil {
		t.Fatalf("delete success should return nil, got: %v", err)
	}
	if !strings.Contains(out, "deleted=1") {
		t.Fatalf("summary should count 1 deleted, output: %q", out)
	}
}

// =============== Fix 2: 跨端分片复制清理用 WithoutCancel ===============

// TestCrossEndpointMultipartAbortSurvivesCancellation 验证跨端分片上传在传输中被
// 取消 (模拟 Ctrl+C) 后, AbortMultipartUpload 仍以未取消的 ctx 发出, 服务端不留残留。
func TestCrossEndpointMultipartAbortSurvivesCancellation(t *testing.T) {
	const totalSize = 6 * 1024 * 1024 // > partSize, 走分片路径
	srcServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodHead:
			w.Header().Set("Content-Length", fmt.Sprint(totalSize))
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			_, _ = io.WriteString(w, strings.Repeat("x", totalSize))
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srcServer.Close()

	var (
		abortCtxErr error
		abortCalled atomic.Bool
		abortOnce   sync.Once
		ctx, cancel = context.WithCancel(context.Background())
	)
	tgt := &Action{S3: actionTestClient(t, "http://example.test", roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		q := r.URL.Query()
		switch {
		case r.Method == http.MethodPost && q.Has("uploads"):
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`<InitiateMultipartUploadResult><UploadId>uid-1</UploadId></InitiateMultipartUploadResult>`)), Request: r}, nil
		case r.Method == http.MethodPut && q.Has("partNumber"):
			// 上传第一个分片时触发取消 (模拟用户 Ctrl+C 打断传输)
			cancel()
			return &http.Response{StatusCode: 500, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`<Error><Code>InternalError</Code></Error>`)), Request: r}, nil
		case r.Method == http.MethodDelete && q.Has("uploadId"):
			abortOnce.Do(func() {
				abortCalled.Store(true)
				// 清理请求的 ctx 必须未被取消: 即 AbortMultipartUpload 用了 WithoutCancel
				abortCtxErr = r.Context().Err()
			})
			return &http.Response{StatusCode: 204, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
		}
		return &http.Response{StatusCode: 500, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`<Error><Code>InternalError</Code></Error>`)), Request: r}, nil
	})), Ctx: ctx}
	src := &Action{S3: actionTestClient(t, srcServer.URL, nil), Ctx: context.Background()}

	err := copyObjectCrossEndpoint(src, tgt, "srcbucket", "big.bin", "tgtbucket", "big.bin", "", 5*1024*1024, nil)
	if err == nil {
		t.Fatal("expected multipart upload failure")
	}
	if !abortCalled.Load() {
		t.Fatal("AbortMultipartUpload was not issued after failure")
	}
	if abortCtxErr != nil {
		t.Fatalf("abort ran with a cancelled context (%v); cleanup must use context.WithoutCancel", abortCtxErr)
	}
}

// =============== Fix 3: MPU 服务端列举翻页 ===============

// mpuPagingServer 返回一个分页的 ListMultipartUploads 服务端:
// 桶内 3 条上传 (a/1.bin, a/2.bin, a/3.bin), 每页最多 2 条,
// 通过 key-marker/upload-id-marker 驱动翻页, 并记录 abort 请求。
func mpuPagingServer(t *testing.T) (*httptest.Server, *[]string, *sync.Mutex) {
	t.Helper()
	all := []struct{ key, id string }{
		{"a/1.bin", "uid-1"},
		{"a/2.bin", "uid-2"},
		{"a/3.bin", "uid-3"},
	}
	var aborted []string
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case q.Has("uploads"):
			keyMarker := q.Get("key-marker")
			idMarker := q.Get("upload-id-marker")
			start := 0
			if keyMarker != "" {
				for i, u := range all {
					if u.key > keyMarker || (u.key == keyMarker && u.id > idMarker) {
						start = i
						break
					}
				}
			}
			page := all[start:]
			var b strings.Builder
			b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><ListMultipartUploadsResult><Bucket>mybucket</Bucket>`)
			truncated := len(page) > 2
			if truncated {
				page = page[:2]
			}
			for _, u := range page {
				fmt.Fprintf(&b, "<Upload><Key>%s</Key><UploadId>%s</UploadId><Initiated>2024-01-02T03:04:05.000Z</Initiated></Upload>", u.key, u.id)
			}
			if truncated {
				next := page[len(page)-1]
				fmt.Fprintf(&b, "<IsTruncated>true</IsTruncated><NextKeyMarker>%s</NextKeyMarker><NextUploadIdMarker>%s</NextUploadIdMarker>", next.key, next.id)
			} else {
				b.WriteString("<IsTruncated>false</IsTruncated>")
			}
			b.WriteString("</ListMultipartUploadsResult>")
			w.Header().Set("Content-Type", "application/xml")
			_, _ = io.WriteString(w, b.String())
		case r.Method == http.MethodDelete && q.Has("uploadId"):
			mu.Lock()
			aborted = append(aborted, q.Get("uploadId"))
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			httpError(w, http.StatusBadRequest, "InvalidRequest", "unsupported")
		}
	}))
	return server, &aborted, &mu
}

func TestMpuListPaginatesAllUploads(t *testing.T) {
	server, _, _ := mpuPagingServer(t)
	defer server.Close()
	client := &Action{S3: actionTestClient(t, server.URL, nil), Alias: "test", Ctx: context.Background()}
	out := captureStdout(t, func() {
		if err := client.MpuList(MpuListOptions{JSON: true}, "mybucket", "a/"); err != nil {
			t.Error(err)
		}
	})
	lines := jsonLines(t, out)
	if len(lines) != 3 {
		t.Fatalf("mpu list returned %d uploads, want 3 (must paginate)\n%s", len(lines), out)
	}
	seen := map[string]bool{}
	for _, l := range lines {
		seen[l["uploadId"].(string)] = true
	}
	for _, id := range []string{"uid-1", "uid-2", "uid-3"} {
		if !seen[id] {
			t.Errorf("upload %s missing from paged listing", id)
		}
	}
}

func TestMpuAbortPaginatesAllUploads(t *testing.T) {
	server, aborted, mu := mpuPagingServer(t)
	defer server.Close()
	client := &Action{S3: actionTestClient(t, server.URL, nil), Alias: "test", Ctx: context.Background()}
	if err := client.MpuAbort("mybucket", "a/", ""); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(*aborted) != 3 {
		t.Fatalf("aborted %d uploads, want 3 (must collect all pages before aborting): %v", len(*aborted), *aborted)
	}
	for _, id := range []string{"uid-1", "uid-2", "uid-3"} {
		if !containsStr(*aborted, id) {
			t.Errorf("upload %s not aborted", id)
		}
	}
}

func TestFindUploadKeyPaginatesAcrossPages(t *testing.T) {
	server, _, _ := mpuPagingServer(t)
	defer server.Close()
	client := &Action{S3: actionTestClient(t, server.URL, nil), Alias: "test", Ctx: context.Background()}

	// 目标 uploadId 在第 2 页: 必须翻页才能找到
	key, err := client.findUploadKey("mybucket", "a/", "uid-3")
	if err != nil {
		t.Fatal(err)
	}
	if key != "a/3.bin" {
		t.Fatalf("findUploadKey(uid-3) = %q, want a/3.bin", key)
	}
	// 不存在的 uploadId -> 空串
	key, err = client.findUploadKey("mybucket", "a/", "nope")
	if err != nil || key != "" {
		t.Fatalf("findUploadKey(missing) = %q, %v; want empty", key, err)
	}
}

// TestRmIncompletePaginatesAllUploads 验证 rm -I (deletePrefixIncomplete) 同样翻页,
// 不漏掉第 2 页的上传。
func TestRmIncompletePaginatesAllUploads(t *testing.T) {
	server, aborted, mu := mpuPagingServer(t)
	defer server.Close()
	client := &Action{S3: actionTestClient(t, server.URL, nil), Alias: "test", Ctx: context.Background()}
	if err := client.DeleteObjects("mybucket", "a/", DelOptions{Recursive: true, Force: true, Incomplete: true}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(*aborted) != 3 {
		t.Fatalf("rm -I aborted %d uploads, want 3: %v", len(*aborted), *aborted)
	}
}

// =============== Fix 4: stat 用量列举错误传播 ===============

// TestStatBucketPropagatesUsageListError 验证无 ListBucket 权限时,
// stat bucket 的用量统计不再静默为 0, 而是返回列举错误。
func TestStatBucketPropagatesUsageListError(t *testing.T) {
	denyList := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case q.Has("location"):
			_, _ = io.WriteString(w, `<LocationConstraint>us-east-1</LocationConstraint>`)
		case q.Has("versioning"):
			w.WriteHeader(http.StatusNotFound)
		case q.Has("policy"):
			httpError(w, http.StatusNotFound, "NoSuchBucketPolicy", "no policy")
		case q.Has("lifecycle"):
			httpError(w, http.StatusNotFound, "NoSuchLifecycleConfiguration", "no lifecycle")
		case q.Get("list-type") == "2" && denyList:
			httpError(w, http.StatusForbidden, "AccessDenied", "no ListBucket permission")
		case q.Get("list-type") == "2":
			_, _ = io.WriteString(w, `<ListBucketResult><IsTruncated>false</IsTruncated></ListBucketResult>`)
		case q.Has("versions"):
			_, _ = io.WriteString(w, `<ListVersionsResult><IsTruncated>false</IsTruncated></ListVersionsResult>`)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()
	client := &Action{S3: actionTestClient(t, server.URL, nil), Alias: "test", Ctx: context.Background()}

	// 失败分支: 列举被拒 -> 必须返回错误 (旧实现静默输出 0 用量)
	err := client.StatObjects(StatOptions{}, "bucket", "")
	if err == nil {
		t.Fatal("expected error when usage listing is denied")
	}
	if !strings.Contains(err.Error(), "list objects") {
		t.Fatalf("error should mention listing, got: %v", err)
	}

	// 成功分支: 允许列举 -> 返回 nil
	denyList = false
	if err := client.StatObjects(StatOptions{}, "bucket", ""); err != nil {
		t.Fatalf("usage listing should succeed: %v", err)
	}
}

// =============== Fix 5: put 扫描感知取消 + RunStream join 扫描协程 ===============

// TestRunStreamWaitsForScanGoroutinesOnCancel 验证 ctx 取消后 RunStream 返回前
// 扫描协程 (含排空 relay 的 drain 协程) 已全部结束, 无后台协程泄漏。
func TestRunStreamWaitsForScanGoroutinesOnCancel(t *testing.T) {
	var scanReturned atomic.Bool
	ctx, cancel := context.WithCancel(context.Background())
	cfg := StreamConfig{
		Concurrency: 2,
		NoProgress:  true,
		Label:       "test",
		Scan: func(ctx context.Context, jobs chan<- StreamJob) error {
			defer scanReturned.Store(true)
			for i := 0; ; i++ {
				select {
				case jobs <- StreamJob{Src: fmt.Sprintf("f%d", i), Dst: "d", Size: 1}:
				case <-ctx.Done():
					// 模拟取消后仍有一段时间的收尾工作 (如文件系统遍历返回前)
					time.Sleep(150 * time.Millisecond)
					return ctx.Err()
				}
			}
		},
		Work: func(ctx context.Context, job StreamJob, report func(n int64)) error {
			return nil
		},
	}
	// 扫描运行一小段后取消
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	err := RunStream(ctx, cfg)
	if err == nil || !IsCanceled(err) {
		t.Fatalf("expected cancellation error, got %v", err)
	}
	if !scanReturned.Load() {
		t.Fatal("scan goroutine still running after RunStream returned (scan goroutine leak)")
	}
}

// TestPutDirectoryScanStopsOnCancel 验证 put 目录上传的 filepath.Walk 感知取消,
// 取消后立即停止遍历, RunStream 返回取消错误而不是遍历完整目录。
func TestPutDirectoryScanStopsOnCancel(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 30; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%d.txt", i)), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := &Action{S3: nil, Alias: "test", Ctx: ctx}
	err := client.uploadDirStreaming(PutOptions{NoProgress: true}, "bucket", "prefix/", dir)
	if err == nil || !IsCanceled(err) {
		t.Fatalf("expected cancellation error, got %v", err)
	}
}

// =============== Fix 6: cp/mv 目录复制跳过目录占位对象 ===============

// dirMarkerServer 提供一个含目录占位对象的源桶: dir/a.txt, dir/empty/ (0 字节),
// dir/sub/c.txt; 目标前缀 out/ 为空。记录 CopyObject (x-amz-copy-source) 与 DELETE。
func dirMarkerServer(t *testing.T, copies, deletes *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		prefix := q.Get("prefix")
		switch {
		case r.Method == http.MethodHead:
			httpError(w, http.StatusNotFound, "NoSuchKey", "not found")
		case q.Get("list-type") == "2" && (prefix == "dir" || prefix == "dir/"):
			_, _ = io.WriteString(w, `<ListBucketResult><IsTruncated>false</IsTruncated>`+
				`<Contents><Key>dir/a.txt</Key><Size>3</Size></Contents>`+
				`<Contents><Key>dir/empty/</Key><Size>0</Size></Contents>`+
				`<Contents><Key>dir/sub/c.txt</Key><Size>5</Size></Contents>`+
				`</ListBucketResult>`)
		case q.Get("list-type") == "2":
			_, _ = io.WriteString(w, `<ListBucketResult><IsTruncated>false</IsTruncated></ListBucketResult>`)
		case r.Method == http.MethodPut && r.Header.Get("x-amz-copy-source") != "":
			copies.Add(1)
			_, _ = io.WriteString(w, `<CopyObjectResult><ETag>"e"</ETag></CopyObjectResult>`)
		case r.Method == http.MethodDelete:
			deletes.Add(1)
			w.WriteHeader(http.StatusNoContent)
		default:
			httpError(w, http.StatusBadRequest, "InvalidRequest", "unsupported")
		}
	}))
}

func TestCopyDirSkipsDirMarkerObjects(t *testing.T) {
	var copies, deletes atomic.Int32
	server := dirMarkerServer(t, &copies, &deletes)
	defer server.Close()
	client := &Action{S3: actionTestClient(t, server.URL, nil), Alias: "test", Ctx: context.Background()}
	if err := client.CopyObjects(CopyOptions{Recursive: true, NoProgress: true}, "bucket", "dir", "bucket", "out/"); err != nil {
		t.Fatal(err)
	}
	if copies.Load() != 2 {
		t.Fatalf("copied %d objects, want 2 (0-byte dir marker dir/empty/ must be skipped)", copies.Load())
	}
}

func TestMvDirSkipsDirMarkerObjects(t *testing.T) {
	var copies, deletes atomic.Int32
	server := dirMarkerServer(t, &copies, &deletes)
	defer server.Close()
	client := &Action{S3: actionTestClient(t, server.URL, nil), Alias: "test", Ctx: context.Background()}
	if err := client.Mv(CopyOptions{Recursive: true, NoProgress: true}, "bucket", "dir", "bucket", "out/"); err != nil {
		t.Fatal(err)
	}
	if copies.Load() != 2 {
		t.Fatalf("copied %d objects, want 2 (0-byte dir marker dir/empty/ must be skipped)", copies.Load())
	}
	if deletes.Load() != 2 {
		t.Fatalf("deleted %d source objects, want 2 (dir marker must not be moved)", deletes.Load())
	}
}

// =============== Fix 7: 断点续传 NoSuchUpload 自愈 ===============

// mpuResumeServer 是断点续传测试的通用服务端, 各字段控制分片操作的行为。
type mpuResumeServer struct {
	srv *httptest.Server

	mu               sync.Mutex
	listPartsErrCode string // 非空时 ListParts 返回该错误码 (404)
	listPartsXML     string // ListParts 成功响应体; 空时返回空列表
	createUploadID   string // CreateMultipartUpload 返回的 UploadId
	uploadPartFails  bool   // UploadPart 是否返回 500

	createCount     int
	uploadPartCount int
	completeCount   int
	completeUID     string
	abortCount      int
}

func newMpuResumeServer(t *testing.T) *mpuResumeServer {
	t.Helper()
	s := &mpuResumeServer{createUploadID: "fresh-uid"}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		s.mu.Lock()
		defer s.mu.Unlock()
		switch {
		case r.Method == http.MethodGet && q.Has("uploadId"):
			if s.listPartsErrCode != "" {
				httpError(w, http.StatusNotFound, s.listPartsErrCode, "list parts failed")
				return
			}
			body := s.listPartsXML
			if body == "" {
				body = `<ListPartsResult><Bucket>mybucket</Bucket><Key>key.bin</Key><UploadId>` + q.Get("uploadId") + `</UploadId><IsTruncated>false</IsTruncated></ListPartsResult>`
			}
			w.Header().Set("Content-Type", "application/xml")
			_, _ = io.WriteString(w, body)
		case r.Method == http.MethodPost && q.Has("uploads"):
			s.createCount++
			_, _ = io.WriteString(w, `<InitiateMultipartUploadResult><UploadId>`+s.createUploadID+`</UploadId></InitiateMultipartUploadResult>`)
		case r.Method == http.MethodPut && q.Has("partNumber"):
			s.uploadPartCount++
			if s.uploadPartFails {
				httpError(w, http.StatusInternalServerError, "InternalError", "upload part failed")
				return
			}
			w.Header().Set("ETag", fmt.Sprintf(`"etag-%s"`, q.Get("partNumber")))
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && q.Has("uploadId"):
			s.completeCount++
			s.completeUID = q.Get("uploadId")
			_, _ = io.WriteString(w, `<CompleteMultipartUploadResult><ETag>"final"</ETag></CompleteMultipartUploadResult>`)
		case r.Method == http.MethodDelete && q.Has("uploadId"):
			s.abortCount++
			w.WriteHeader(http.StatusNoContent)
		default:
			httpError(w, http.StatusBadRequest, "InvalidRequest", "unsupported")
		}
	}))
	return s
}

// writeResumeState 写入一份与本地文件匹配的断点续传状态文件。
func writeResumeState(t *testing.T, localPath, uploadID string, fi os.FileInfo) string {
	t.Helper()
	statePath, err := multipartStatePath(localPath, "mybucket", "key.bin")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		t.Fatal(err)
	}
	st := multipartState{
		Version: 1, UploadID: uploadID, Bucket: "mybucket", Key: "key.bin",
		LocalPath: localPath, PartSize: minMultipartPartSize, TotalSize: fi.Size(),
		ModTimeUnixNs: fi.ModTime().UnixNano(),
	}
	if err := os.WriteFile(statePath, mustJSON(st), 0o600); err != nil {
		t.Fatal(err)
	}
	return statePath
}

func runResumeUpload(t *testing.T, s *mpuResumeServer, localPath string) error {
	t.Helper()
	client := &Action{S3: actionTestClient(t, s.srv.URL, nil), Ctx: context.Background()}
	f, err := os.Open(localPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	return client.uploadMultipartFile(context.Background(), "mybucket", "key.bin", localPath, f, fi, 1, &s3iface.PutObjectOptions{}, nil)
}

func smallLocalFile(t *testing.T) string {
	t.Helper()
	local := filepath.Join(t.TempDir(), "big.bin")
	if err := os.WriteFile(local, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	return local
}

// TestUploadMultipartFileFreshUploadSucceeds 无状态文件时的全新上传成功路径。
func TestUploadMultipartFileFreshUploadSucceeds(t *testing.T) {
	setupMpuHome(t)
	local := smallLocalFile(t)
	s := newMpuResumeServer(t)
	defer s.srv.Close()
	if err := runResumeUpload(t, s, local); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	if s.completeCount != 1 || s.uploadPartCount != 1 {
		t.Fatalf("complete=%d uploadParts=%d, want 1/1", s.completeCount, s.uploadPartCount)
	}
	s.mu.Unlock()
	statePath, _ := multipartStatePath(local, "mybucket", "key.bin")
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("state file should be removed after success (err=%v)", err)
	}
}

// TestUploadMultipartFileResumesListedParts 成功分支: 服务端已有已上传分片时续传,
// 不再重复上传已存在的分片。
func TestUploadMultipartFileResumesListedParts(t *testing.T) {
	setupMpuHome(t)
	local := smallLocalFile(t)
	fi, _ := os.Stat(local)
	statePath := writeResumeState(t, local, "resume-uid", fi)
	s := newMpuResumeServer(t)
	defer s.srv.Close()
	s.mu.Lock()
	s.listPartsXML = `<ListPartsResult><Bucket>mybucket</Bucket><Key>key.bin</Key><UploadId>resume-uid</UploadId><IsTruncated>false</IsTruncated><Part><PartNumber>1</PartNumber><ETag>etag-1</ETag></Part></ListPartsResult>`
	s.mu.Unlock()

	if err := runResumeUpload(t, s, local); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	if s.completeCount != 1 || s.uploadPartCount != 0 {
		t.Fatalf("complete=%d uploadParts=%d, want 1/0 (existing part must be reused)", s.completeCount, s.uploadPartCount)
	}
	if s.completeUID != "resume-uid" {
		t.Fatalf("completed with uploadId %q, want resume-uid", s.completeUID)
	}
	s.mu.Unlock()
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("state file should be removed after success")
	}
}

// TestUploadMultipartFileSelfHealsNoSuchUpload 失败分支: 服务端分片上传已不存在
// (NoSuchUpload) 时, 放弃旧 uploadID 重新创建并继续, 上传整体不失败。
func TestUploadMultipartFileSelfHealsNoSuchUpload(t *testing.T) {
	setupMpuHome(t)
	local := smallLocalFile(t)
	fi, _ := os.Stat(local)
	statePath := writeResumeState(t, local, "stale-uid", fi)
	s := newMpuResumeServer(t)
	defer s.srv.Close()
	s.mu.Lock()
	s.listPartsErrCode = "NoSuchUpload" // 服务端 upload 已被 Abort/过期清理
	s.createUploadID = "fresh-uid"
	s.mu.Unlock()

	if err := runResumeUpload(t, s, local); err != nil {
		t.Fatalf("NoSuchUpload should self-heal instead of failing: %v", err)
	}
	s.mu.Lock()
	if s.completeUID != "fresh-uid" {
		t.Fatalf("completed with uploadId %q, want fresh-uid (must recreate after NoSuchUpload)", s.completeUID)
	}
	if s.createCount != 1 {
		t.Fatalf("create count = %d, want 1", s.createCount)
	}
	s.mu.Unlock()
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("state file should be removed after success")
	}
}

// TestUploadMultipartFileListPartsErrorHintsLocalClear 失败分支: 其它 ListParts
// 错误返回错误, 且错误信息提示可用 mpu local-clear 清理本地状态。
func TestUploadMultipartFileListPartsErrorHintsLocalClear(t *testing.T) {
	setupMpuHome(t)
	local := smallLocalFile(t)
	fi, _ := os.Stat(local)
	statePath := writeResumeState(t, local, "uid-1", fi)
	s := newMpuResumeServer(t)
	defer s.srv.Close()
	s.mu.Lock()
	s.listPartsErrCode = "InternalError" // 非 NoSuchUpload 的瞬时错误
	s.mu.Unlock()

	err := runResumeUpload(t, s, local)
	if err == nil {
		t.Fatal("expected error for non-NoSuchUpload ListParts failure")
	}
	if !strings.Contains(err.Error(), "mpu local-clear") {
		t.Fatalf("error should hint `mpu local-clear`, got: %v", err)
	}
	// 本地状态文件保留, 便于稍后重试
	if _, statErr := os.Stat(statePath); statErr != nil {
		t.Fatalf("state file should be preserved for later retry: %v", statErr)
	}
}

// =============== Fix 8: rm --include/--exclude 相对 key 匹配 ===============

// TestRmIncludeExcludeMatchesRelativeKeys 验证 rm 的 --include/--exclude 与 mirror
// 一致, 按「相对参数前缀的相对 key」匹配 (而非完整 key)。
func TestRmIncludeExcludeMatchesRelativeKeys(t *testing.T) {
	cases := []struct {
		name       string
		include    []string
		exclude    []string
		wantDelete []string // 批量删除请求中应包含的 key
	}{
		{"no filters", nil, nil, []string{"dir/a.txt", "dir/sub/c.txt"}},
		{"relative include with slash", []string{"sub/*"}, nil, []string{"dir/sub/c.txt"}},
		{"basename include", []string{"a.txt"}, nil, []string{"dir/a.txt"}},
		{"exclude basename", nil, []string{"*.txt"}, nil},
		// 行为变更说明: 按完整 key 写 "dir/*" 在相对基准下匹配不到任何对象
		// (相对 key 是 "a.txt"/"sub/c.txt"), 与 mirror 的语义保持一致。
		{"absolute-style include matches nothing", []string{"dir/*"}, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var batchDeleted []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				q := r.URL.Query()
				switch {
				case q.Get("list-type") == "2" && q.Get("prefix") == "dir/":
					_, _ = io.WriteString(w, `<ListBucketResult><IsTruncated>false</IsTruncated>`+
						`<Contents><Key>dir/a.txt</Key><Size>3</Size></Contents>`+
						`<Contents><Key>dir/sub/c.txt</Key><Size>5</Size></Contents>`+
						`</ListBucketResult>`)
				case r.Method == http.MethodPost && q.Has("delete"):
					var req struct {
						Objects []struct {
							Key string `xml:"Key"`
						} `xml:"Object"`
					}
					_ = xml.NewDecoder(r.Body).Decode(&req)
					mu.Lock()
					for _, o := range req.Objects {
						batchDeleted = append(batchDeleted, o.Key)
					}
					mu.Unlock()
					_, _ = io.WriteString(w, `<DeleteResult></DeleteResult>`)
				case r.Method == http.MethodDelete:
					w.WriteHeader(http.StatusNoContent)
				default:
					httpError(w, http.StatusBadRequest, "InvalidRequest", "unsupported")
				}
			}))
			defer server.Close()
			client := &Action{S3: actionTestClient(t, server.URL, nil), Alias: "test", Ctx: context.Background()}
			opt := DelOptions{Recursive: true, Force: true, Include: tc.include, Exclude: tc.exclude}
			if err := client.DeleteObjects("bucket", "dir/", opt); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if !sameStringSet(batchDeleted, tc.wantDelete) {
				t.Fatalf("batch-deleted keys = %v, want %v", batchDeleted, tc.wantDelete)
			}
		})
	}
}

func sameStringSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	m := map[string]bool{}
	for _, k := range want {
		m[k] = true
	}
	for _, k := range got {
		if !m[k] {
			return false
		}
	}
	return true
}

// =============== Fix 9: checkIfDirectory 前缀碰撞误判 ===============

// TestIsS3FileDoesNotMisjudgePrefixCollision 验证不存在且同名前缀只有
// "report-2023.pdf" 这类对象时, "report" 不会被误判为目录。
func TestIsS3FileDoesNotMisjudgePrefixCollision(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case r.Method == http.MethodHead && strings.TrimPrefix(r.URL.Path, "/bucket/") == "report-2023.pdf":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodHead:
			httpError(w, http.StatusNotFound, "NoSuchKey", "not found")
		case q.Get("list-type") == "2":
			prefix := q.Get("prefix")
			switch prefix {
			case "dir/":
				_, _ = io.WriteString(w, `<ListBucketResult><IsTruncated>false</IsTruncated><Contents><Key>dir/a.txt</Key></Contents></ListBucketResult>`)
			case "report":
				// 裸前缀会命中 report-2023.pdf —— 旧实现靠这一层兜底把 report 误判为目录
				_, _ = io.WriteString(w, `<ListBucketResult><IsTruncated>false</IsTruncated><Contents><Key>report-2023.pdf</Key></Contents></ListBucketResult>`)
			default:
				_, _ = io.WriteString(w, `<ListBucketResult><IsTruncated>false</IsTruncated></ListBucketResult>`)
			}
		default:
			httpError(w, http.StatusBadRequest, "InvalidRequest", "unsupported")
		}
	}))
	defer server.Close()
	client := &Action{S3: actionTestClient(t, server.URL, nil), Alias: "test", Ctx: context.Background()}

	// 真实文件 -> file
	ok, err := client.IsS3File("bucket", "report-2023.pdf")
	if err != nil || !ok {
		t.Fatalf("IsS3File(file) = %v, %v; want true, nil", ok, err)
	}
	// 目录 (裸前缀 + 带尾斜杠都应识别为目录)
	for _, key := range []string{"dir", "dir/"} {
		ok, err = client.IsS3File("bucket", key)
		if err != nil || ok {
			t.Fatalf("IsS3File(%q) = %v, %v; want false, nil (directory)", key, ok, err)
		}
	}
	// 不存在的文件名, 即使存在同名前缀对象 (report-2023.pdf) 也不得判为目录
	ok, err = client.IsS3File("bucket", "report")
	if err == nil {
		t.Fatalf("IsS3File(report) = %v, nil; want error (not a directory)", ok)
	}
}

// =============== Fix 10: needsUpdate 秒级 mtime 注释 (无行为变更) ===============

// TestNeedsUpdateSecondPrecision 钉住 needsUpdate 在 MPU (ETag 不可比) 下的
// 秒级精度行为: 同一秒内的 mtime 视为相同, 不做更新 (S3 LastModified 局限)。
func TestNeedsUpdateSecondPrecision(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	src := ObjectInfo{ETag: "abc-1", Size: 10, LastModified: now}
	tgt := ObjectInfo{ETag: "def-1", Size: 10, LastModified: now}
	if needsUpdate(src, tgt) {
		t.Fatal("same second-level mtime must not trigger update (S3 LastModified second precision)")
	}
}
