package action

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"s3cli/pkg/s3api"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type failingBody struct{ sent bool }

func (b *failingBody) Read(p []byte) (int, error) {
	if !b.sent {
		b.sent = true
		copy(p, "new")
		return 3, nil
	}
	return 0, errors.New("network interrupted")
}
func (*failingBody) Close() error { return nil }

func actionTestClient(t *testing.T, endpoint string, transport http.RoundTripper) *s3api.Client {
	t.Helper()
	c, err := s3api.New(&s3api.Options{Endpoint: endpoint, AccessKey: "access", SecretKey: "secret", Transport: transport, MaxRetries: 0})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestDownloadFileAtomicallyReplacesOnlyAfterSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "new-content") }))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(path, []byte("old-content"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &S3Client{S3: actionTestClient(t, server.URL, nil), Ctx: context.Background()}
	if _, err := client.downloadFile("key", path, "bucket", nil); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "new-content" {
		t.Fatalf("target = %q", data)
	}

	failing := &S3Client{S3: actionTestClient(t, "http://example.test", roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: &failingBody{}, Request: r}, nil
	})), Ctx: context.Background()}
	if err := os.WriteFile(path, []byte("preserved"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := failing.downloadFile("key", path, "bucket", nil); err == nil {
		t.Fatal("expected download failure")
	}
	data, _ = os.ReadFile(path)
	if string(data) != "preserved" {
		t.Fatalf("existing target was overwritten: %q", data)
	}
}

func TestMirrorCopyFailureSkipsRemove(t *testing.T) {
	var deletes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Query().Has("delete") {
			deletes.Add(1)
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `<Error><Code>InternalError</Code></Error>`)
	}))
	defer server.Close()
	api := actionTestClient(t, server.URL, nil)
	src := &S3Client{S3: api, Alias: "src", Ctx: context.Background()}
	tgt := &S3Client{S3: api, Alias: "tgt", Ctx: context.Background()}
	plan := &mirrorPlan{cfg: MirrorOptions{Remove: true, NoProgress: true, Concurrency: 1}, srcClient: src, tgtClient: tgt, srcBucket: "source", tgtBucket: "target", sameEP: true}
	actions := make(chan diffAction, 2)
	actions <- diffAction{rel: "copy", size: 1}
	actions <- diffAction{rel: "extra", delete: true}
	close(actions)
	if err := plan.copyAndDelete(actions, make(chan error), nil); err == nil {
		t.Fatal("expected copy failure")
	}
	if deletes.Load() != 0 {
		t.Fatalf("remove executed after copy failure: %d", deletes.Load())
	}
}

func TestRecursiveDeleteAndCancelledMirrorDoNotDeleteUnexpectedly(t *testing.T) {
	var batchDeletes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Query().Get("list-type") == "2":
			_, _ = io.WriteString(w, `<ListBucketResult><IsTruncated>false</IsTruncated><Contents><Key>prefix/a</Key></Contents><Contents><Key>prefix/b</Key></Contents></ListBucketResult>`)
		case r.URL.Query().Has("delete"):
			batchDeletes.Add(1)
			_, _ = io.WriteString(w, `<DeleteResult><Deleted><Key>prefix/a</Key></Deleted><Deleted><Key>prefix/b</Key></Deleted></DeleteResult>`)
		case r.Method == http.MethodDelete:
			// 目录标记删除请求 (DELETE /bucket/prefix/)
			_, _ = io.WriteString(w, "")
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()
	api := actionTestClient(t, server.URL, nil)
	client := &S3Client{S3: api, Alias: "test", Ctx: context.Background()}
	if err := client.DeleteObjects("bucket", "prefix/", DelOptions{Recursive: true}); err != nil {
		t.Fatal(err)
	}
	if batchDeletes.Load() != 1 {
		t.Fatalf("batch deletes = %d", batchDeletes.Load())
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	plan := &mirrorPlan{cfg: MirrorOptions{Remove: true, NoProgress: true, Concurrency: 1}, srcClient: &S3Client{S3: api, Ctx: cancelled}, tgtClient: &S3Client{S3: api, Ctx: cancelled}, srcBucket: "source", tgtBucket: "target"}
	actions := make(chan diffAction, 1)
	actions <- diffAction{rel: "extra", delete: true}
	close(actions)
	before := batchDeletes.Load()
	if err := plan.copyAndDelete(actions, make(chan error), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error = %v", err)
	}
	if batchDeletes.Load() != before {
		t.Fatal("cancelled mirror issued a delete request")
	}
}

func TestDeleteObjectRemovesEmptyParentDirectoryMarkers(t *testing.T) {
	var deleted []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodHead && r.URL.Path == "/bucket/parent/child/object":
			w.WriteHeader(http.StatusOK)
		case r.URL.Query().Get("list-type") == "2" && r.URL.Query().Get("prefix") == "parent/child/":
			_, _ = io.WriteString(w, `<ListBucketResult><IsTruncated>false</IsTruncated><Contents><Key>parent/child/</Key></Contents></ListBucketResult>`)
		case r.URL.Query().Get("list-type") == "2" && r.URL.Query().Get("prefix") == "parent/":
			_, _ = io.WriteString(w, `<ListBucketResult><IsTruncated>false</IsTruncated><Contents><Key>parent/</Key></Contents><Contents><Key>parent/other</Key></Contents></ListBucketResult>`)
		case r.Method == http.MethodDelete:
			deleted = append(deleted, r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()
	api := actionTestClient(t, server.URL, nil)
	client := &S3Client{S3: api, Alias: "test", Ctx: context.Background()}
	if err := client.DeleteObjects("bucket", "parent/child/object", DelOptions{}); err != nil {
		t.Fatal(err)
	}

	if got, want := strings.Join(deleted, ","), "/bucket/parent/child/object,/bucket/parent/child/"; got != want {
		t.Fatalf("deleted paths = %q, want %q", got, want)
	}
}

func TestDeleteBatchReportsObjectErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("delete") {
			_, _ = io.WriteString(w, `<DeleteResult><Error><Key>protected</Key><Code>AccessDenied</Code><Message>denied</Message></Error></DeleteResult>`)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	client := &S3Client{S3: actionTestClient(t, server.URL, nil), Ctx: context.Background()}
	err := client.deleteBatch("bucket", []s3api.ObjectIdentifier{{Key: "protected"}})
	if err == nil || !strings.Contains(err.Error(), `"protected": AccessDenied: denied`) {
		t.Fatalf("delete batch error = %v", err)
	}
}

func TestMirrorMaxDeleteBlocksBeforeDelete(t *testing.T) {
	var deletes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("delete") {
			deletes.Add(1)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	api := actionTestClient(t, server.URL, nil)
	client := &S3Client{S3: api, Ctx: context.Background()}
	plan := &mirrorPlan{cfg: MirrorOptions{Remove: true, MaxDelete: 1, NoProgress: true, Concurrency: 1}, srcClient: client, tgtClient: client, srcBucket: "source", tgtBucket: "target"}
	actions := make(chan diffAction, 2)
	actions <- diffAction{rel: "a", delete: true}
	actions <- diffAction{rel: "b", delete: true}
	close(actions)
	if err := plan.copyAndDelete(actions, make(chan error), nil); err == nil {
		t.Fatal("expected max-delete error")
	}
	if deletes.Load() != 0 {
		t.Fatal("max-delete guard still issued a delete request")
	}
}

// TestPutSkipsExistingObjectByDefault 验证 put 默认不覆盖: 目标对象已存在时跳过,
// 加 --overwrite 才强制上传。
func TestPutSkipsExistingObjectByDefault(t *testing.T) {
	var puts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			puts.Add(1)
		}
		w.Header().Set("ETag", `"etag"`)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	dir := t.TempDir()
	local := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(local, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &S3Client{S3: actionTestClient(t, server.URL, nil), Alias: "test", Ctx: context.Background()}

	// 默认 (Overwrite=false): 目标已存在 -> 跳过, 不应发起 PUT
	if err := client.PutObject(PutOptions{}, "bucket", "", local, false); err != nil {
		t.Fatalf("expected skip, got error: %v", err)
	}
	if puts.Load() != 0 {
		t.Fatalf("expected no upload, got %d PUT(s)", puts.Load())
	}

	// --overwrite: 目标已存在仍强制上传 -> 应发起 PUT
	if err := client.PutObject(PutOptions{Overwrite: true}, "bucket", "", local, false); err != nil {
		t.Fatalf("expected upload, got error: %v", err)
	}
	if puts.Load() != 1 {
		t.Fatalf("expected 1 upload, got %d", puts.Load())
	}
}

// TestGetSkipsExistingLocalFileByDefault 验证 get 默认不覆盖: 本地文件已存在时跳过,
// 加 --overwrite 才强制下载。
func TestGetSkipsExistingLocalFileByDefault(t *testing.T) {
	var gets atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			gets.Add(1)
			_, _ = io.WriteString(w, "remote-content")
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	local := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(local, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &S3Client{S3: actionTestClient(t, server.URL, nil), Alias: "test", Ctx: context.Background()}

	// 默认 (Overwrite=false): 本地已存在 -> 跳过, 不应发起 GET, 内容保持不变
	if err := client.downloadSingleFile(GetOptions{}, "bucket", "file.txt", local); err != nil {
		t.Fatalf("expected skip, got error: %v", err)
	}
	if gets.Load() != 0 {
		t.Fatalf("expected no download, got %d GET(s)", gets.Load())
	}
	data, _ := os.ReadFile(local)
	if string(data) != "existing" {
		t.Fatalf("existing file was modified: %q", data)
	}

	// --overwrite: 本地已存在仍强制下载
	if err := client.downloadSingleFile(GetOptions{Overwrite: true}, "bucket", "file.txt", local); err != nil {
		t.Fatalf("expected download, got error: %v", err)
	}
	if gets.Load() != 1 {
		t.Fatalf("expected 1 download, got %d", gets.Load())
	}
	data, _ = os.ReadFile(local)
	if string(data) != "remote-content" {
		t.Fatalf("file not overwritten: %q", data)
	}
}

func TestMirrorPlanRejectsOverlappingPrefixes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// HeadObject / ListObjectsV2 一律返回 "不存在", DestStateOf 走 DestNone 分支
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	newClient := func() *S3Client {
		return &S3Client{S3: actionTestClient(t, server.URL, nil), Ctx: context.Background()}
	}
	cases := []struct {
		name      string
		srcPrefix string
		tgtPrefix string
		wantErr   bool
	}{
		{"tgt inside src", "", "sub/", true},
		{"src inside tgt", "dir/", "", true},
		{"nested tgt", "dir/", "dir/sub/", true},
		{"identical", "dir/", "dir/", true},
		{"sibling prefixes ok", "a/", "b/", false},
		{"dir vs dir2 ok", "dir/", "dir2/", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := MirrorOptions{
				Src: &S3PathOptions{Client: newClient(), Bucket: "bkt", ObjectKey: tc.srcPrefix, TrailingSlash: true},
				Tgt: &S3PathOptions{Client: newClient(), Bucket: "bkt", ObjectKey: tc.tgtPrefix, TrailingSlash: true},
			}
			_, err := resolveMirrorPlan(cfg)
			if tc.wantErr && err == nil {
				t.Fatalf("expected overlap error for %q vs %q", tc.srcPrefix, tc.tgtPrefix)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error for %q vs %q: %v", tc.srcPrefix, tc.tgtPrefix, err)
			}
		})
	}
}

// TestMirrorPlanNormalizesPrefixes 验证裸前缀 ("dir") 被规范化为 "dir/",
// 避免列举时前缀碰撞 ("dir2/x" 被误同步, "out2/y" 被 --remove 误删)。
func TestMirrorPlanNormalizesPrefixes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	cfg := MirrorOptions{
		Src: &S3PathOptions{Client: &S3Client{S3: actionTestClient(t, server.URL, nil), Ctx: context.Background()}, Bucket: "bkt", ObjectKey: "dir"},
		// 同 endpoint 不同 bucket: 不触发 overlap 守卫, 且 DestStateOf 快速返回
		Tgt: &S3PathOptions{Client: &S3Client{S3: actionTestClient(t, server.URL, nil), Ctx: context.Background()}, Bucket: "bkt2", ObjectKey: "out"},
	}
	plan, err := resolveMirrorPlan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if plan.srcPrefix != "dir/" || plan.tgtPrefix != "out/" {
		t.Fatalf("prefixes = %q, %q; want dir/, out/", plan.srcPrefix, plan.tgtPrefix)
	}
	if relKey("dir/a.txt", plan.srcPrefix) != "a.txt" {
		t.Fatal("relKey after normalization failed")
	}
}
