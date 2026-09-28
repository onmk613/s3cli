// resume_checksum_test.go 锁定断点续传的分片校验和回填语义。
//
// 背景: 带 --checksum 的分片上传在断点续传时, 从 ListParts 重建的
// CompletedPart 只带回 PartNumber+ETag。创建上传时声明了算法的服务端 (AWS)
// 会在 Complete 阶段校验每个分片的校验和, 丢失它会导致 Complete 被拒绝
// 或静默绕过整片校验。
package action

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"s3cli/internal/api"
)

// TestResumeCarriesPartChecksumsToComplete: ListParts 返回的分片校验和
// 必须原样出现在 CompleteMultipartUpload 请求体中。
func TestResumeCarriesPartChecksumsToComplete(t *testing.T) {
	setupMpuHome(t)
	local := smallLocalFile(t) // "0123456789" -> 1 个分片
	fi, err := os.Stat(local)
	if err != nil {
		t.Fatal(err)
	}
	writeResumeState(t, local, "resume-crc", fi)

	var (
		mu           sync.Mutex
		completeBody string
		uploadParts  int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case r.Method == http.MethodGet && q.Has("uploadId"):
			_, _ = io.WriteString(w, `<ListPartsResult><Bucket>mybucket</Bucket><Key>key.bin</Key><UploadId>resume-crc</UploadId><IsTruncated>false</IsTruncated>`+
				`<Part><PartNumber>1</PartNumber><ETag>etag-1</ETag><Size>10</Size><ChecksumCRC32>crc32-base64-1</ChecksumCRC32></Part>`+
				`</ListPartsResult>`)
		case r.Method == http.MethodPost && q.Has("uploads"):
			_, _ = io.WriteString(w, `<InitiateMultipartUploadResult><UploadId>fresh-uid</UploadId></InitiateMultipartUploadResult>`)
		case r.Method == http.MethodPut && q.Has("partNumber"):
			mu.Lock()
			uploadParts++
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && q.Has("uploadId"):
			body, _ := io.ReadAll(r.Body)
			mu.Lock()
			completeBody = string(body)
			mu.Unlock()
			_, _ = io.WriteString(w, `<CompleteMultipartUploadResult><ETag>"final"</ETag></CompleteMultipartUploadResult>`)
		case r.Method == http.MethodDelete && q.Has("uploadId"):
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unsupported", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	client := &Action{S3: actionTestClient(t, srv.URL, nil), Ctx: context.Background()}
	f, err := os.Open(local)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	fi, err = f.Stat()
	if err != nil {
		t.Fatal(err)
	}

	opts := &api.PutObjectOptions{ChecksumAlgorithm: api.ChecksumCRC32}
	if err := client.uploadMultipartFile(context.Background(), "mybucket", "key.bin", local, f, fi, 1, opts, nil); err != nil {
		t.Fatalf("resume upload: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if uploadParts != 0 {
		t.Fatalf("uploadParts=%d, want 0 (existing part must be reused)", uploadParts)
	}
	if !strings.Contains(completeBody, "<ChecksumCRC32>crc32-base64-1</ChecksumCRC32>") {
		t.Fatalf("CompleteMultipartUpload body is missing the resumed part checksum:\n%s", fmt.Sprintf("%.400s", completeBody))
	}
	if !strings.Contains(completeBody, "<ETag>etag-1</ETag>") {
		t.Fatalf("CompleteMultipartUpload body is missing the resumed part ETag:\n%s", fmt.Sprintf("%.400s", completeBody))
	}
}

// TestMPULockBlocksConcurrentAcquire: 同一目标的锁被持有时,
// 第二个获取必须立即失败 (跨进程互斥; flock 按 fd 计锁, 同进程同样冲突)。
func TestMPULockBlocksConcurrentAcquire(t *testing.T) {
	setupMpuHome(t)
	statePath, err := multipartStatePath("/tmp/file.bin", "mybucket", "key.bin")
	if err != nil {
		t.Fatal(err)
	}

	lock1, err := lockMultipartState(statePath)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	_, err = lockMultipartState(statePath)
	if err == nil {
		t.Fatal("second acquire must fail while the lock is held")
	}
	lock1.Release()

	// 释放后可重新获取。
	lock2, err := lockMultipartState(statePath)
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	lock2.Release()
	// Release 幂等。
	lock2.Release()
}
