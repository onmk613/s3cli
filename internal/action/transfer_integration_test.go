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
