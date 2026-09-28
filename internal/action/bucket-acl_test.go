// bucket-acl_test.go 覆盖 `bucket acl set` / `object acl set` 的参数校验与
// 实际发出的 HTTP 请求 (路径 / 查询参数 / x-amz-acl 与 x-amz-grant-* 头)。
//
// 校验必须在本地完成: 一个"什么都没设置"的 PUT ?acl 会静默失败或被服务端拒绝,
// 不如直接报错并指明缺少哪些 flag。

package action

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// aclRecorder 记录 mock 服务端收到的最后一次请求。
type aclRecorder struct {
	requests atomic.Int32
	method   string
	path     string
	query    string
	header   http.Header
}

func newACLTestAction(t *testing.T) (*Action, *aclRecorder) {
	t.Helper()
	rec := &aclRecorder{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.requests.Add(1)
		rec.method = r.Method
		rec.path = r.URL.Path
		rec.query = r.URL.RawQuery
		rec.header = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	return &Action{S3: actionTestClient(t, server.URL, nil), Alias: "test", Ctx: context.Background()}, rec
}

// TestSetBucketACLRequiresAtLeastOneFlag 未给任何 ACL 参数时必须本地报错, 且不发出请求。
func TestSetBucketACLRequiresAtLeastOneFlag(t *testing.T) {
	a, rec := newACLTestAction(t)

	err := a.SetBucketACL(ACLSetOptions{}, "mybucket")
	if err == nil {
		t.Fatal("expected an error when no ACL flag is given")
	}
	if !strings.Contains(err.Error(), "--acl") {
		t.Errorf("error should point at the missing flags: %v", err)
	}
	if rec.requests.Load() != 0 {
		t.Fatalf("no request should be issued, got %d", rec.requests.Load())
	}
}

// TestSetBucketACLRejectsBadFlags 参数取值非法时在本地拦下 (同样不发请求)。
func TestSetBucketACLRejectsBadFlags(t *testing.T) {
	cases := []struct {
		name string
		opt  ACLSetOptions
		want string
	}{
		{"未知 canned ACL", ACLSetOptions{ACL: "public-everything"}, "public-everything"},
		{"grantee 类型未知", ACLSetOptions{GrantRead: []string{"user=bob"}}, "user=bob"},
		{"grantee 取值为空", ACLSetOptions{GrantWrite: []string{"id="}}, "id="},
		{"grantee 含引号", ACLSetOptions{GrantRead: []string{`id=a"b`}}, "invalid grantee"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, rec := newACLTestAction(t)
			err := a.SetBucketACL(tc.opt, "mybucket")
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to mention %q", err, tc.want)
			}
			if rec.requests.Load() != 0 {
				t.Fatalf("no request should be issued, got %d", rec.requests.Load())
			}
		})
	}
}

// TestSetBucketACLSendsCannedACLAndGrantHeaders 校验 PUT /bucket?acl 及其头。
func TestSetBucketACLSendsCannedACLAndGrantHeaders(t *testing.T) {
	a, rec := newACLTestAction(t)

	err := a.SetBucketACL(ACLSetOptions{
		ACL:              "public-read",
		GrantRead:        []string{"id=abc123", "http://acs.amazonaws.com/groups/global/AllUsers"},
		GrantFullControl: []string{"owner@example.com"},
	}, "mybucket")
	if err != nil {
		t.Fatal(err)
	}

	if rec.requests.Load() != 1 {
		t.Fatalf("requests = %d, want 1", rec.requests.Load())
	}
	if rec.method != http.MethodPut {
		t.Errorf("method = %s, want PUT", rec.method)
	}
	if rec.path != "/mybucket" {
		t.Errorf("path = %q, want /mybucket", rec.path)
	}
	if !strings.Contains(rec.query, "acl") {
		t.Errorf("query = %q, want it to carry the acl subresource", rec.query)
	}
	want := map[string]string{
		"x-amz-acl":                "public-read",
		"x-amz-grant-read":         `id="abc123", uri="http://acs.amazonaws.com/groups/global/AllUsers"`,
		"x-amz-grant-full-control": `emailAddress="owner@example.com"`,
	}
	for name, value := range want {
		if got := rec.header.Get(name); got != value {
			t.Errorf("%s = %q, want %q", name, got, value)
		}
	}
	// 未给出的 grant 头不应出现
	for _, name := range []string{"x-amz-grant-write", "x-amz-grant-read-acp", "x-amz-grant-write-acp"} {
		if got := rec.header.Get(name); got != "" {
			t.Errorf("%s = %q, want it to be unset", name, got)
		}
	}
}

// TestSetObjectACLRequiresKeyAndCarriesVersionID 对象级 ACL: 必须有 key, 且带上 versionId。
func TestSetObjectACLRequiresKeyAndCarriesVersionID(t *testing.T) {
	a, rec := newACLTestAction(t)

	if err := a.SetObjectACL(ACLSetOptions{ACL: "private"}, "mybucket", "", ""); err == nil {
		t.Fatal("expected an error for a bare bucket path")
	}
	if rec.requests.Load() != 0 {
		t.Fatalf("no request should be issued for a bare bucket, got %d", rec.requests.Load())
	}

	if err := a.SetObjectACL(ACLSetOptions{ACL: "private"}, "mybucket", "dir/obj.txt", "v1"); err != nil {
		t.Fatal(err)
	}
	if rec.path != "/mybucket/dir/obj.txt" {
		t.Errorf("path = %q, want /mybucket/dir/obj.txt", rec.path)
	}
	if !strings.Contains(rec.query, "acl") || !strings.Contains(rec.query, "versionId=v1") {
		t.Errorf("query = %q, want acl + versionId=v1", rec.query)
	}
	if got := rec.header.Get("x-amz-acl"); got != "private" {
		t.Errorf("x-amz-acl = %q, want private", got)
	}
}

// TestGetBucketACLOutput GET ?acl 的两种输出: --json 包装为 {"acl": "<xml>"},
// 默认输出把单行 XML 缩进展开并保留 xsi: 前缀。
func TestGetBucketACLOutput(t *testing.T) {
	const aclXML = `<?xml version="1.0" encoding="UTF-8"?><AccessControlPolicy xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Owner><ID>abc</ID></Owner><AccessControlList><Grant><Grantee xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:type="CanonicalUser"><ID>abc</ID></Grantee><Permission>FULL_CONTROL</Permission></Grant></AccessControlList></AccessControlPolicy>`

	var gotMethod, gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotQuery = r.Method, r.URL.RawQuery
		_, _ = io.WriteString(w, aclXML)
	}))
	t.Cleanup(server.Close)
	a := &Action{S3: actionTestClient(t, server.URL, nil), Alias: "test", Ctx: context.Background()}

	out := captureStdout(t, func() {
		if err := a.GetBucketACL(ACLGetOptions{JSON: true}, "mybucket"); err != nil {
			t.Error(err)
		}
	})
	var doc map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v (%q)", err, out)
	}
	if len(doc) != 1 || doc["acl"] != aclXML {
		t.Errorf("json doc = %#v, want exactly {\"acl\": <raw xml>}", doc)
	}
	if gotMethod != http.MethodGet || !strings.Contains(gotQuery, "acl") {
		t.Errorf("request = %s ?%s, want GET ?acl", gotMethod, gotQuery)
	}

	out = captureStdout(t, func() {
		if err := a.GetBucketACL(ACLGetOptions{}, "mybucket"); err != nil {
			t.Error(err)
		}
	})
	if !strings.Contains(out, "\n  <Owner>") {
		t.Errorf("default output should be indented:\n%s", out)
	}
	if !strings.Contains(out, `xsi:type="CanonicalUser"`) {
		t.Errorf("pretty printing must keep namespace prefixes:\n%s", out)
	}
}

// TestPrettyXMLFallsBackToRaw 非法 XML 不被改写 (调用方回退原样输出)。
func TestPrettyXMLFallsBackToRaw(t *testing.T) {
	if pretty, ok := prettyXML([]byte("not xml at all")); ok {
		t.Errorf("plain text should not be treated as XML: %q", pretty)
	}
	if pretty, ok := prettyXML(nil); ok {
		t.Errorf("empty body should not be treated as XML: %q", pretty)
	}
	if pretty, ok := prettyXML([]byte("<a><b></a>")); ok {
		t.Errorf("malformed XML should not be treated as XML: %q", pretty)
	}
}
