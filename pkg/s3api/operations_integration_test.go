package s3api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestObjectMultipartDeleteAndBucketConfigurationAPIs(t *testing.T) {
	var mu sync.Mutex
	var requests []string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.EscapedPath()+"?"+r.URL.RawQuery+" "+string(body))
		mu.Unlock()
		q := r.URL.Query()
		switch {
		case q.Has("uploads"):
			_, _ = io.WriteString(w, `<InitiateMultipartUploadResult><UploadId>upload-1</UploadId></InitiateMultipartUploadResult>`)
		case q.Get("uploadId") == "upload-1" && q.Has("partNumber"):
			w.Header().Set("ETag", `"part-etag"`)
		case q.Get("uploadId") == "upload-1" && r.Method == http.MethodPost:
			_, _ = io.WriteString(w, `<CompleteMultipartUploadResult><Bucket>bucket</Bucket><Key>large</Key><ETag>"final"</ETag></CompleteMultipartUploadResult>`)
		case q.Get("uploadId") == "abort":
			w.WriteHeader(http.StatusNoContent)
		case q.Has("delete"):
			_, _ = io.WriteString(w, `<DeleteResult><Deleted><Key>a</Key><VersionId>v1</VersionId></Deleted><Error><Key>b</Key><Code>AccessDenied</Code></Error></DeleteResult>`)
		case q.Has("cors") && r.Method == http.MethodGet:
			_, _ = io.WriteString(w, `<CORSConfiguration><CORSRule><AllowedMethod>GET</AllowedMethod><AllowedOrigin>*</AllowedOrigin></CORSRule></CORSConfiguration>`)
		case q.Has("policy") && r.Method == http.MethodGet:
			_, _ = io.WriteString(w, `{"Version":"2012-10-17"}`)
		case q.Has("encryption") && r.Method == http.MethodGet:
			_, _ = io.WriteString(w, `<ServerSideEncryptionConfiguration><Rule><ApplyServerSideEncryptionByDefault><SSEAlgorithm>AES256</SSEAlgorithm></ApplyServerSideEncryptionByDefault></Rule></ServerSideEncryptionConfiguration>`)
		case q.Has("notification") && r.Method == http.MethodGet:
			_, _ = io.WriteString(w, `<NotificationConfiguration><TopicConfiguration><Topic>arn:test</Topic><Event>s3:ObjectCreated:*</Event></TopicConfiguration></NotificationConfiguration>`)
		case q.Has("versioning") && r.Method == http.MethodGet:
			_, _ = io.WriteString(w, `<VersioningConfiguration><Status>Enabled</Status></VersioningConfiguration>`)
		default:
			w.Header().Set("ETag", `"object-etag"`)
			w.Header().Set("x-amz-version-id", "v1")
			w.Header().Set("x-amz-delete-marker", "true")
		}
	})
	s := httptest.NewServer(h)
	defer s.Close()
	c, err := New(&Options{Endpoint: s.URL, AccessKey: "access", SecretKey: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	put, err := c.PutObject(ctx, "bucket", "object", []byte("data"), &PutObjectOptions{ContentType: "text/plain"})
	if err != nil || put.ETag != "object-etag" || put.VersionID != "v1" {
		t.Fatalf("put=%#v err=%v", put, err)
	}
	init, err := c.CreateMultipartUpload(ctx, "bucket", "large", nil)
	if err != nil || init.UploadID != "upload-1" {
		t.Fatalf("init=%#v err=%v", init, err)
	}
	part, err := c.UploadPart(ctx, "bucket", "large", init.UploadID, 1, []byte("part"))
	if err != nil || part.ETag != "part-etag" {
		t.Fatalf("part=%#v err=%v", part, err)
	}
	complete, err := c.CompleteMultipartUpload(ctx, "bucket", "large", init.UploadID, []CompletedPart{{PartNumber: 1, ETag: part.ETag}})
	if err != nil || complete.ETag != "final" {
		t.Fatalf("complete=%#v err=%v", complete, err)
	}
	if err := c.AbortMultipartUpload(ctx, "bucket", "abandoned", "abort"); err != nil {
		t.Fatal(err)
	}
	deleted, err := c.DeleteObject(ctx, "bucket", "object", "version")
	if err != nil || deleted.VersionID != "v1" || !deleted.DeleteMarker {
		t.Fatalf("delete=%#v err=%v", deleted, err)
	}
	batch, err := c.DeleteObjects(ctx, "bucket", []ObjectIdentifier{{Key: "a", VersionID: "v1"}, {Key: "b"}}, false)
	if err != nil || len(batch.Deleted) != 1 || len(batch.Errors) != 1 {
		t.Fatalf("batch=%#v err=%v", batch, err)
	}

	corsConfig := &CorsConfig{CORSRules: []CorsRule{{AllowedMethod: []string{"GET"}, AllowedOrigin: []string{"*"}}}}
	if err := c.SetBucketCors(ctx, "bucket", corsConfig); err != nil {
		t.Fatal(err)
	}
	if got, err := c.GetBucketCors(ctx, "bucket"); err != nil || len(got.CORSRules) != 1 {
		t.Fatalf("cors=%#v err=%v", got, err)
	}
	if err := c.DeleteBucketCors(ctx, "bucket"); err != nil {
		t.Fatal(err)
	}
	policy := []byte(`{"Version":"2012-10-17"}`)
	if err := c.SetBucketPolicy(ctx, "bucket", policy); err != nil {
		t.Fatal(err)
	}
	if got, err := c.GetBucketPolicy(ctx, "bucket"); err != nil || string(got) != string(policy) {
		t.Fatalf("policy=%q err=%v", got, err)
	}
	if err := c.DeleteBucketPolicy(ctx, "bucket"); err != nil {
		t.Fatal(err)
	}
	if err := c.SetBucketEncryption(ctx, "bucket", &ServerSideEncryptionConfiguration{Rules: []ServerSideEncryptionRule{{ApplyServerSideEncryptionByDefault: ServerSideEncryptionByDefault{SSEAlgorithm: "AES256"}}}}); err != nil {
		t.Fatal(err)
	}
	if got, err := c.GetBucketEncryption(ctx, "bucket"); err != nil || got.Rules[0].ApplyServerSideEncryptionByDefault.SSEAlgorithm != "AES256" {
		t.Fatalf("encryption=%#v err=%v", got, err)
	}
	if err := c.DeleteBucketEncryption(ctx, "bucket"); err != nil {
		t.Fatal(err)
	}
	if err := c.SetBucketNotification(ctx, "bucket", &NotificationConfiguration{}); err != nil {
		t.Fatal(err)
	}
	if got, err := c.GetBucketNotification(ctx, "bucket"); err != nil || got.TopicConfigurations[0].TopicARN != "arn:test" {
		t.Fatalf("notification=%#v err=%v", got, err)
	}
	if err := c.DeleteBucketNotification(ctx, "bucket"); err != nil {
		t.Fatal(err)
	}
	if err := c.SetBucketVersioning(ctx, "bucket", VersioningEnabled); err != nil {
		t.Fatal(err)
	}
	if got, err := c.GetBucketVersioning(ctx, "bucket"); err != nil || got != VersioningEnabled {
		t.Fatalf("versioning=%q err=%v", got, err)
	}

	mu.Lock()
	joined := strings.Join(requests, "\n")
	mu.Unlock()
	for _, want := range []string{"PUT /bucket/object?", "POST /bucket/large?uploads=", "DELETE /bucket/abandoned?uploadId=abort", "POST /bucket?delete=", "PUT /bucket?cors="} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing request %q in:\n%s", want, joined)
		}
	}
}
