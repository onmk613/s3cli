// bucket-public-access_test.go 覆盖 `bucket public-access-block set`:
// 命令行未设置的开关不得被"顺带打开" —— 只有显式给出的项才会写入请求体。

package action

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

type pabRecorder struct {
	requests atomic.Int32
	method   string
	path     string
	query    string
	body     string
}

func newPABTestAction(t *testing.T) (*Action, *pabRecorder) {
	t.Helper()
	rec := &pabRecorder{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.requests.Add(1)
		body, _ := io.ReadAll(r.Body)
		rec.method = r.Method
		rec.path = r.URL.Path
		rec.query = r.URL.RawQuery
		rec.body = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	return &Action{S3: actionTestClient(t, server.URL, nil), Alias: "test", Ctx: context.Background()}, rec
}

// pabBoolPtr 避免与其它测试文件的通用 helper 重名。
func pabBoolPtr(b bool) *bool { return &b }

// TestSetPublicAccessBlockRequiresAtLeastOneFlag 一项都没给时报错, 且不发请求。
func TestSetPublicAccessBlockRequiresAtLeastOneFlag(t *testing.T) {
	a, rec := newPABTestAction(t)

	err := a.SetPublicAccessBlock(PublicAccessBlockOptions{}, "mybucket")
	if err == nil {
		t.Fatal("expected an error when no flag is given")
	}
	if !strings.Contains(err.Error(), "--block-public-acls") {
		t.Errorf("error should point at the missing flags: %v", err)
	}
	if rec.requests.Load() != 0 {
		t.Fatalf("no request should be issued, got %d", rec.requests.Load())
	}
}

// TestSetPublicAccessBlockSendsOnlyChangedFlags 只给出一个开关时, 其余开关保持 false,
// 请求为 PUT /bucket?publicAccessBlock + XML body。
func TestSetPublicAccessBlockSendsOnlyChangedFlags(t *testing.T) {
	a, rec := newPABTestAction(t)

	if err := a.SetPublicAccessBlock(PublicAccessBlockOptions{BlockPublicACLs: pabBoolPtr(true)}, "mybucket"); err != nil {
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
	if !strings.Contains(rec.query, "publicAccessBlock") {
		t.Errorf("query = %q, want the publicAccessBlock subresource", rec.query)
	}
	if !strings.Contains(rec.body, "<BlockPublicAcls>true</BlockPublicAcls>") {
		t.Errorf("body should enable BlockPublicAcls:\n%s", rec.body)
	}
	// 只有显式给出的那一项为 true, 其余三项保持 false。
	if n := strings.Count(rec.body, ">true<"); n != 1 {
		t.Errorf("body has %d true flags, want exactly 1:\n%s", n, rec.body)
	}
	for _, elem := range []string{"IgnorePublicAcls", "BlockPublicPolicy", "RestrictPublicBuckets"} {
		if !strings.Contains(rec.body, "<"+elem+">false</"+elem+">") {
			t.Errorf("body should keep %s=false:\n%s", elem, rec.body)
		}
	}
}

// TestSetPublicAccessBlockHonoursExplicitFalse 显式 =false 与未设置语义不同:
// 显式 false 会被写进配置 (同时另一个开关为 true)。
func TestSetPublicAccessBlockHonoursExplicitFalse(t *testing.T) {
	a, rec := newPABTestAction(t)

	err := a.SetPublicAccessBlock(PublicAccessBlockOptions{
		BlockPublicACLs:   pabBoolPtr(false),
		BlockPublicPolicy: pabBoolPtr(true),
	}, "mybucket")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rec.body, "<BlockPublicAcls>false</BlockPublicAcls>") {
		t.Errorf("explicit false should be sent:\n%s", rec.body)
	}
	if !strings.Contains(rec.body, "<BlockPublicPolicy>true</BlockPublicPolicy>") {
		t.Errorf("BlockPublicPolicy should be enabled:\n%s", rec.body)
	}
	if n := strings.Count(rec.body, ">true<"); n != 1 {
		t.Errorf("body has %d true flags, want exactly 1:\n%s", n, rec.body)
	}
}

// TestBuildPublicAccessBlockConfigUnsetIsFalse 未设置的指针一律按 false 处理,
// 保证 nil (未设置) 永远不会被当作 true 发送。
func TestBuildPublicAccessBlockConfigUnsetIsFalse(t *testing.T) {
	cfg, err := buildPublicAccessBlockConfig(PublicAccessBlockOptions{RestrictPublicBuckets: pabBoolPtr(true)})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BlockPublicAcls || cfg.IgnorePublicAcls || cfg.BlockPublicPolicy {
		t.Errorf("unset flags should stay false: %+v", cfg)
	}
	if !cfg.RestrictPublicBuckets {
		t.Errorf("explicitly set flag should be true: %+v", cfg)
	}
}
