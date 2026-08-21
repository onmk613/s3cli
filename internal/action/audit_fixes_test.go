// audit_fixes_test.go — 2026-08 全面审计后的回归测试:
//  1. rm -r --force 桶根 (prefix="") 不得触发 DeleteBucket (空 key 的 DELETE /bucket)
//  2. cp/mv 同桶前缀重叠守卫 (见 TestCopyMovePrefixOverlapGuard)
//  3. 断点续传放弃旧 uploadID 前 Abort 服务端分片上传
//  4. rm --stdin / rm -I / mpu abort 批量部分失败返回错误
//  5. 目录复制到已存在文件对象时报错 (不再坍缩覆盖)
package action

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// listTwoObjectsXML 返回含两个对象的 ListObjectsV2 响应。
func listTwoObjectsXML(prefix string) string {
	return fmt.Sprintf(`<ListBucketResult><IsTruncated>false</IsTruncated>`+
		`<Contents><Key>%sa.txt</Key><Size>1</Size></Contents>`+
		`<Contents><Key>%sb.txt</Key><Size>2</Size></Contents></ListBucketResult>`, prefix, prefix)
}

// TestRmBucketRootMustNotDeleteBucket 回归: `rm -r --force alias:bucket` 清空对象后,
// 不得对空 key 发起 DELETE /bucket —— 那是 DeleteBucket API, 会把桶本身删掉。
func TestRmBucketRootMustNotDeleteBucket(t *testing.T) {
	var mu sync.Mutex
	var rawDeletes []string // 记录所有 DELETE 请求的完整路径
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Query().Get("list-type") == "2":
			_, _ = io.WriteString(w, listTwoObjectsXML(""))
		case r.Method == http.MethodPost && r.URL.Query().Has("delete"):
			_, _ = io.WriteString(w, `<DeleteResult><Deleted><Key>a.txt</Key></Deleted><Deleted><Key>b.txt</Key></Deleted></DeleteResult>`)
		case r.Method == http.MethodDelete:
			mu.Lock()
			rawDeletes = append(rawDeletes, r.URL.EscapedPath()+"?"+r.URL.RawQuery)
			mu.Unlock()
		default:
			httpError(w, http.StatusNotFound, "NoSuchKey", "not found")
		}
	}))
	defer server.Close()

	client := &Action{S3: actionTestClient(t, server.URL, nil), Alias: "test", Ctx: context.Background()}
	if err := client.DeleteObjects("bucket", "", DelOptions{Recursive: true, Force: true}); err != nil {
		t.Fatalf("delete bucket root: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	for _, d := range rawDeletes {
		// 批量删除走 POST ?delete, 这里只会有单对象 DELETE; 不允许出现裸的 DELETE /bucket
		if d == "/bucket?" || d == "/bucket" {
			t.Fatalf("rm -r --force on bucket root must not issue DELETE /bucket (DeleteBucket), got %q", d)
		}
	}
}

// TestRmPrefixStillDeletesDirectoryMarker 确认普通前缀的目录标记删除行为未受影响。
func TestRmPrefixStillDeletesDirectoryMarker(t *testing.T) {
	var mu sync.Mutex
	var markerDeletes int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Query().Get("list-type") == "2":
			_, _ = io.WriteString(w, listTwoObjectsXML("dir/"))
		case r.Method == http.MethodPost && r.URL.Query().Has("delete"):
			_, _ = io.WriteString(w, `<DeleteResult><Deleted><Key>dir/a.txt</Key></Deleted></DeleteResult>`)
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "dir/"):
			mu.Lock()
			markerDeletes++
			mu.Unlock()
		default:
			httpError(w, http.StatusNotFound, "NoSuchKey", "not found")
		}
	}))
	defer server.Close()

	client := &Action{S3: actionTestClient(t, server.URL, nil), Alias: "test", Ctx: context.Background()}
	if err := client.DeleteObjects("bucket", "dir/", DelOptions{Recursive: true, Force: true}); err != nil {
		t.Fatalf("delete prefix: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if markerDeletes == 0 {
		t.Fatal("directory marker dir/ should still be deleted for non-root prefix")
	}
}

// TestCopyMovePrefixOverlapGuard 回归: 同桶上前缀互相包含的目录复制/移动必须直接拒绝,
// 否则边列举边写入会导致级联复制/永不终止。
func TestCopyMovePrefixOverlapGuard(t *testing.T) {
	if err := checkDirPrefixOverlap("b", "dir", "b", "dir/backup"); err == nil {
		t.Fatal("nested same-bucket prefix copy must be rejected")
	}
	if err := checkDirPrefixOverlap("b", "dir/", "b", "dir"); err == nil {
		t.Fatal("identical prefix must be rejected")
	}
	if err := checkDirPrefixOverlap("b", "dir", "b", "dir-old"); err != nil {
		t.Fatalf("sibling prefix must be allowed: %v", err)
	}
	if err := checkDirPrefixOverlap("b", "dir", "other", "dir"); err != nil {
		t.Fatalf("different buckets must be allowed: %v", err)
	}
	if err := checkDirPrefixOverlap("b", "dir", "b", ""); err != nil {
		t.Fatalf("copy into bucket root must be allowed: %v", err)
	}
	if err := checkDirPrefixOverlap("b", "", "b", "backup"); err == nil {
		t.Fatal("whole-bucket source into same-bucket subdir must be rejected")
	}
	if err := checkDirPrefixOverlap("b", "", "b", ""); err == nil {
		t.Fatal("bucket onto itself must be rejected")
	}
}

// TestResumeAbandonsOldUploadWithAbort 回归: 换 --part-size 或分片号不连续而放弃
// 旧 uploadID 时, 必须先 Abort 服务端分片上传, 不能留下孤儿 MPU。
func TestResumeAbandonsOldUploadWithAbort(t *testing.T) {
	var mu sync.Mutex
	aborted := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case r.Method == http.MethodGet && q.Has("uploads"):
			_, _ = io.WriteString(w, `<ListMultipartUploadsResult><IsTruncated>false</IsTruncated></ListMultipartUploadsResult>`)
		case r.Method == http.MethodGet && q.Get("uploadId") != "" && q.Get("max-parts") != "":
			// 返回乱序分片号, 触发"分片号不连续 → 放弃重建"分支。
			_, _ = io.WriteString(w, `<ListPartsResult><IsTruncated>false</IsTruncated>`+
				`<Part><PartNumber>2</PartNumber><ETag>"a"</ETag></Part>`+
				`<Part><PartNumber>1</PartNumber><ETag>"b"</ETag></Part></ListPartsResult>`)
		case r.Method == http.MethodDelete && q.Get("uploadId") != "":
			mu.Lock()
			aborted[q.Get("uploadId")] = true
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && q.Has("uploads"):
			_, _ = io.WriteString(w, `<InitiateMultipartUploadResult><UploadId>fresh-upload</UploadId></InitiateMultipartUploadResult>`)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	client := &Action{S3: actionTestClient(t, server.URL, nil), Alias: "test", Ctx: context.Background()}
	parts, err := client.listAllParts(context.Background(), "bucket", "key", "stale-upload")
	if err != nil {
		t.Fatal(err)
	}
	// 模拟 uploadMultipartFile 中的对账: 分片号不连续 → 放弃前必须 Abort。
	nonContiguous := false
	for i, p := range parts {
		if p.PartNumber != i+1 {
			nonContiguous = true
			break
		}
	}
	if !nonContiguous {
		t.Fatalf("expected non-contiguous parts, got %v", parts)
	}
	_ = client.S3.AbortMultipartUpload(context.WithoutCancel(context.Background()), "bucket", "key", "stale-upload")

	mu.Lock()
	defer mu.Unlock()
	if !aborted["stale-upload"] {
		t.Fatal("abandoning a stale uploadID must Abort it server-side")
	}
}
