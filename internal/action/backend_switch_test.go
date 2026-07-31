// backend_switch_test.go 验证 internal/action 可在自建请求 (s3api) 与官方 SDK (awss3)
// 两种后端间无缝切换: 同一组 action 操作分别用两个后端跑同一套行为断言, 结果必须一致.
//
// mockS3Server 是内存版的最小 S3 兼容服务, 同时被 s3api.Client 与 awss3.AWS 访问.

package action

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"

	"s3cli/pkg/awss3"
	"s3cli/pkg/s3api"
	"s3cli/pkg/s3iface"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// mockObject 是 mock 服务器中一个对象条目.
type mockS3Server struct {
	mu      sync.Mutex
	objects map[string]string // key -> body
}

func newMockS3Server() *mockS3Server {
	return &mockS3Server{objects: map[string]string{
		"dir/a.txt":     "alpha",
		"dir/b.txt":     "beta",
		"dir/sub/c.txt": "gamma",
		"paginate/k1":   "1",
		"paginate/k2":   "2",
		"paginate/k3":   "3",
		"paginate/k4":   "4",
		"paginate/k5":   "5",
	}}
}

// httpError 写出标准 S3 XML 错误响应.
func httpError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, "<Error><Code>%s</Code><Message>%s</Message></Error>", code, msg)
}

func (m *mockS3Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	bucket := parts[0]
	key := strings.Join(parts[1:], "/")
	q := r.URL.Query()

	switch {
	case bucket == "":
		// ListBuckets
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><ListAllMyBucketsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Buckets><Bucket><Name>mybucket</Name><CreationDate>2024-01-01T00:00:00.000Z</CreationDate></Bucket></Buckets></ListAllMyBucketsResult>`))

	case q.Get("list-type") == "2":
		// ListObjectsV2
		prefix := q.Get("prefix")
		delimiter := q.Get("delimiter")
		token := q.Get("continuation-token")

		m.mu.Lock()
		var keys []string
		for k := range m.objects {
			if strings.HasPrefix(k, prefix) {
				keys = append(keys, k)
			}
		}
		m.mu.Unlock()
		sort.Strings(keys)

		// 分页测试: prefix=paginate/ 时每页 2 个
		pageSize := len(keys)
		start := 0
		var truncated bool
		var nextToken string
		if strings.HasPrefix(prefix, "paginate/") {
			pageSize = 2
			if token != "" {
				fmt.Sscanf(token, "page-%d", &start)
			}
			if start+pageSize < len(keys) {
				truncated = true
				nextToken = fmt.Sprintf("page-%d", start+pageSize)
				keys = keys[start : start+pageSize]
			} else {
				keys = keys[start:]
			}
		}

		var contents, prefixes []string
		if delimiter != "" {
			for _, k := range keys {
				if rest := strings.TrimPrefix(k, prefix); strings.Contains(rest, delimiter) {
					cp := prefix + strings.SplitN(rest, delimiter, 2)[0] + delimiter
					if !containsStr(prefixes, cp) {
						prefixes = append(prefixes, cp)
					}
				} else {
					contents = append(contents, k)
				}
			}
		} else {
			contents = keys
		}

		var b strings.Builder
		b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">`)
		fmt.Fprintf(&b, "<Name>%s</Name><Prefix>%s</Prefix>", bucket, prefix)
		if delimiter != "" {
			fmt.Fprintf(&b, "<Delimiter>%s</Delimiter>", delimiter)
		}
		for _, k := range contents {
			fmt.Fprintf(&b, "<Contents><Key>%s</Key><Size>%d</Size><ETag>&quot;etag-%s&quot;</ETag></Contents>", k, len(k), k)
		}
		for _, p := range prefixes {
			fmt.Fprintf(&b, "<CommonPrefixes><Prefix>%s</Prefix></CommonPrefixes>", p)
		}
		if truncated {
			b.WriteString("<IsTruncated>true</IsTruncated>")
			fmt.Fprintf(&b, "<NextContinuationToken>%s</NextContinuationToken>", nextToken)
		} else {
			b.WriteString("<IsTruncated>false</IsTruncated>")
		}
		b.WriteString("</ListBucketResult>")
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(b.String()))

	case r.Method == http.MethodPut:
		// PutObject (含分片上传的 CreateMultipartUpload/Put 等也走 PUT)
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		m.mu.Lock()
		m.objects[key] = string(body)
		m.mu.Unlock()
		w.Header().Set("ETag", `"etag-uploaded"`)
		w.WriteHeader(http.StatusOK)

	case r.Method == http.MethodHead:
		m.mu.Lock()
		body, ok := m.objects[key]
		m.mu.Unlock()
		if !ok {
			httpError(w, http.StatusNotFound, "NoSuchKey", "The specified key does not exist.")
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		w.Header().Set("ETag", `"etag"`)
		w.WriteHeader(http.StatusOK)

	case r.Method == http.MethodGet:
		m.mu.Lock()
		body, ok := m.objects[key]
		m.mu.Unlock()
		if !ok {
			httpError(w, http.StatusNotFound, "NoSuchKey", "The specified key does not exist.")
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		_, _ = w.Write([]byte(body))

	case r.Method == http.MethodPost && q.Has("delete"):
		// DeleteObjects: 仅删除请求体里列出的 key
		var req struct {
			Objects []struct {
				Key string `xml:"Key"`
			} `xml:"Object"`
		}
		_ = xml.NewDecoder(r.Body).Decode(&req)
		m.mu.Lock()
		for _, o := range req.Objects {
			delete(m.objects, o.Key)
		}
		m.mu.Unlock()
		w.Header().Set("Content-Type", "application/xml")
		var b strings.Builder
		b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><DeleteResult>`)
		for _, o := range req.Objects {
			fmt.Fprintf(&b, "<Deleted><Key>%s</Key></Deleted>", o.Key)
		}
		b.WriteString("</DeleteResult>")
		_, _ = w.Write([]byte(b.String()))

	case r.Method == http.MethodDelete:

	default:
		httpError(w, http.StatusBadRequest, "InvalidRequest", "unsupported request")
	}
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// newDualBackends 同时构造 s3api 与 awss3 两种后端, 指向同一个 mock 服务器.
func newDualBackends(t *testing.T, server *httptest.Server) (s3iface.S3Operations, s3iface.S3Operations) {
	t.Helper()

	builtin, err := s3api.New(&s3api.Options{
		Endpoint:   server.URL,
		AccessKey:  "access",
		SecretKey:  "secret",
		MaxRetries: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	awsClient := s3.New(s3.Options{
		BaseEndpoint:     aws.String(server.URL),
		Region:           "us-east-1",
		Credentials:      credentials.NewStaticCredentialsProvider("access", "secret", ""),
		RetryMaxAttempts: 1,
	})
	official, err := awss3.NewAWS(context.Background(), awsClient)
	if err != nil {
		t.Fatal(err)
	}

	return builtin, official
}

// TestActionBackendParity 用同一套操作断言跑两个后端, 验证 action 层可无缝切换.
func TestActionBackendParity(t *testing.T) {
	server := httptest.NewServer(newMockS3Server())
	defer server.Close()

	builtin, official := newDualBackends(t, server)
	scenarios := []struct {
		name string
		s3   s3iface.S3Operations
	}{
		{"s3api-builtin", builtin},
		{"awss3-official", official},
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			c := &S3Client{S3: sc.s3, Alias: "test", Ctx: context.Background()}

			// 1. ListBuckets
			buckets, err := c.S3.ListBuckets(c.Ctx)
			if err != nil {
				t.Fatalf("ListBuckets: %v", err)
			}
			if len(buckets) != 1 || buckets[0].Name != "mybucket" {
				t.Fatalf("ListBuckets = %+v", buckets)
			}

			// 2. ListObjectsV2: 目录分隔 (CommonPrefixes)
			listResp, err := c.S3.ListObjectsV2(c.Ctx, "mybucket", &s3api.ListObjectsV2Options{Prefix: "dir/", Delimiter: "/"})
			if err != nil {
				t.Fatalf("ListObjectsV2: %v", err)
			}
			if len(listResp.CommonPrefixes) != 1 || listResp.CommonPrefixes[0] != "dir/sub/" {
				t.Fatalf("CommonPrefixes = %v", listResp.CommonPrefixes)
			}
			if len(listResp.Contents) != 2 {
				t.Fatalf("Contents = %d, want 2", len(listResp.Contents))
			}

			// 3. 分页器: paginate/ 前缀每页 2 个, 共 5 个
			var keys []string
			pg := c.S3.NewListObjectsV2Paginator("mybucket", &s3api.ListObjectsV2Options{Prefix: "paginate/"})
			for pg.HasMorePages() {
				page, err := pg.NextPage(c.Ctx)
				if err != nil {
					t.Fatalf("NextPage: %v", err)
				}
				for _, o := range page.Contents {
					keys = append(keys, o.Key)
				}
			}
			if len(keys) != 5 || keys[0] != "paginate/k1" || keys[4] != "paginate/k5" {
				t.Fatalf("paginated keys = %v", keys)
			}

			// 4. IsS3File: 文件 / 目录前缀 / 不存在
			ok, err := c.IsS3File("mybucket", "dir/a.txt")
			if err != nil || !ok {
				t.Fatalf("IsS3File(file) = %v, %v", ok, err)
			}
			ok, err = c.IsS3File("mybucket", "dir/sub")
			if err != nil || ok {
				t.Fatalf("IsS3File(dir-prefix) = %v, %v", ok, err)
			}
			if _, err := c.IsS3File("mybucket", "missing.txt"); err == nil {
				t.Fatal("IsS3File(missing) expected error")
			}

			// 4b. 错误映射: 两个后端都必须产出可识别的 *s3iface.ErrorResponse.
			// 注意: HEAD 无响应体, 官方 SDK 只能按状态码映射为 "NotFound";
			// 自建 s3api 按对象名推断为 "NoSuchKey". action 层对两者同等处理.
			_, err = c.S3.HeadObject(c.Ctx, "mybucket", "missing.txt", "")
			var apiErr *s3iface.ErrorResponse
			if err == nil || !errors.As(err, &apiErr) {
				t.Fatalf("HeadObject(missing) err = %v, want ErrorResponse", err)
			}
			if apiErr.StatusCode != http.StatusNotFound ||
				(apiErr.Code != "NoSuchKey" && apiErr.Code != "NotFound") {
				t.Fatalf("HeadObject(missing) = %+v", apiErr)
			}

			// 4c. GetObject (非 HEAD) 的 404 两个后端都应解析出 NoSuchKey
			_, err = c.S3.GetObject(c.Ctx, "mybucket", "missing.txt", &s3api.GetObjectOptions{})
			var getErr *s3iface.ErrorResponse
			if err == nil || !errors.As(err, &getErr) || getErr.Code != "NoSuchKey" {
				t.Fatalf("GetObject(missing) err = %v, want NoSuchKey ErrorResponse", err)
			}

			// 5. Put + Get 回环
			if _, err := c.S3.PutObject(c.Ctx, "mybucket", "upload.txt", []byte("hello"), &s3api.PutObjectOptions{ContentType: "text/plain"}); err != nil {
				t.Fatalf("PutObject: %v", err)
			}
			getResp, err := c.S3.GetObject(c.Ctx, "mybucket", "upload.txt", &s3api.GetObjectOptions{})
			if err != nil {
				t.Fatalf("GetObject: %v", err)
			}
			data, err := io.ReadAll(getResp.Body)
			_ = getResp.Body.Close()
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			if string(data) != "hello" {
				t.Fatalf("round trip body = %q", data)
			}

			// 6. HeadObject 元数据
			head, err := c.S3.HeadObject(c.Ctx, "mybucket", "dir/a.txt", "")
			if err != nil {
				t.Fatalf("HeadObject: %v", err)
			}
			if head.ContentLength != 5 {
				t.Fatalf("HeadObject.ContentLength = %d", head.ContentLength)
			}

			// 7. DeleteObjects
			delResp, err := c.S3.DeleteObjects(c.Ctx, "mybucket", []s3api.ObjectIdentifier{{Key: "k1"}, {Key: "k2"}}, false)
			if err != nil {
				t.Fatalf("DeleteObjects: %v", err)
			}
			if len(delResp.Deleted) != 2 {
				t.Fatalf("DeleteObjects.Deleted = %d", len(delResp.Deleted))
			}

			// 8. 凭证访问器
			if c.S3.AccessKey() != "access" || c.S3.SecretKey() != "secret" || c.S3.Endpoint() == "" {
				t.Fatalf("creds accessors: key=%q secret=%q endpoint=%q", c.S3.AccessKey(), c.S3.SecretKey(), c.S3.Endpoint())
			}

			// 9. action 层列举 (ListObjects) 不报错
			if err := c.ListObjects("mybucket", "dir/", true); err != nil {
				t.Fatalf("ListObjects: %v", err)
			}
		})
	}
}
