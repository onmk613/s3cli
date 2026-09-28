// lifecycle_flow_test.go 覆盖 bucket-lifecycle.go 的主流程 (set/rule/remove),
// 此前该 854 行文件只有 parseLifecycleConfig 的解析单测。
//
// 手法: 有状态的生命周期 mock 服务端 (PUT ?lifecycle 存储、GET 回放、
// DELETE 清空), 断言每次落库的规则集合与各入口的校验语义。
package action

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"s3cli/internal/api"
)

// lifecycleServer 是一个只实现 lifecycle 子资源的有状态服务端。
type lifecycleServer struct {
	srv    *httptest.Server
	mu     sync.Mutex
	xml    string // 当前 lifecycle 配置 (空 = 未配置)
	puts   []string
	delefe int
}

func newLifecycleServer(t *testing.T) *lifecycleServer {
	t.Helper()
	s := &lifecycleServer{}
	s.srv = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *lifecycleServer) handle(w http.ResponseWriter, r *http.Request) {
	if !r.URL.Query().Has("lifecycle") {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch r.Method {
	case http.MethodPut:
		body, _ := io.ReadAll(r.Body)
		s.puts = append(s.puts, string(body))
		s.xml = string(body)
		w.WriteHeader(http.StatusOK)
	case http.MethodGet:
		if s.xml == "" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `<Error><Code>NoSuchLifecycleConfiguration</Code></Error>`)
			return
		}
		_, _ = io.WriteString(w, s.xml)
	case http.MethodDelete:
		s.xml = ""
		s.delefe++
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusBadRequest)
	}
}

func (s *lifecycleServer) config(t *testing.T) *api.LifecycleConfig {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.xml == "" {
		return &api.LifecycleConfig{}
	}
	cfg, err := parseLifecycleConfig([]byte(s.xml), "xml")
	if err != nil {
		t.Fatalf("parse stored lifecycle xml: %v", err)
	}
	return cfg
}

func (s *lifecycleServer) deleteCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.delefe
}

func lifecycleAction(t *testing.T, s *lifecycleServer) *Action {
	return &Action{S3: actionTestClient(t, s.srv.URL, nil), Alias: "test", Ctx: context.Background()}
}

func ruleIDs(cfg *api.LifecycleConfig) []string {
	ids := make([]string, 0, len(cfg.Rules))
	for _, r := range cfg.Rules {
		ids = append(ids, r.ID)
	}
	return ids
}

// TestSetLifecycleTTL: --prefix+--ttl 生成一条过期规则, 服务端收到 Enabled 状态。
func TestSetLifecycleTTL(t *testing.T) {
	s := newLifecycleServer(t)
	c := lifecycleAction(t, s)

	if err := c.SetLifecycle(LifecycleOptions{Prefix: "logs/", TTL: "30"}, "mybucket"); err != nil {
		t.Fatalf("SetLifecycle: %v", err)
	}
	cfg := s.config(t)
	if len(cfg.Rules) != 1 {
		t.Fatalf("stored %d rules, want 1", len(cfg.Rules))
	}
	rule := cfg.Rules[0]
	if rule.Status != "Enabled" {
		t.Fatalf("rule status = %q, want Enabled", rule.Status)
	}
	if rule.Expiration == nil || rule.Expiration.Days == nil || *rule.Expiration.Days != 30 {
		t.Fatalf("expiration days missing/wrong: %+v", rule.Expiration)
	}
}

// TestSetLifecycleValidation: 两种模式都不给 / TTL 非法 / 文件不存在 必须报错,
// 且不发任何 PUT。
func TestSetLifecycleValidation(t *testing.T) {
	s := newLifecycleServer(t)
	c := lifecycleAction(t, s)

	if err := c.SetLifecycle(LifecycleOptions{}, "mybucket"); err == nil {
		t.Fatal("empty options must be rejected")
	}
	if err := c.SetLifecycle(LifecycleOptions{TTL: "abc"}, "mybucket"); err == nil {
		t.Fatal("non-numeric TTL must be rejected")
	}
	if err := c.SetLifecycle(LifecycleOptions{ConfigFile: filepath.Join(t.TempDir(), "nope.json")}, "mybucket"); err == nil {
		t.Fatal("missing config file must be rejected")
	}
	if len(s.puts) != 0 {
		t.Fatalf("validation failures must not reach the server, puts=%d", len(s.puts))
	}
}

// TestLifecycleRuleUpsertAndRemove: 规则 upsert 幂等; 删最后一条规则时
// 走整份 Delete 而不是空配置 PUT。
func TestLifecycleRuleUpsertAndRemove(t *testing.T) {
	s := newLifecycleServer(t)
	c := lifecycleAction(t, s)

	// 初始: 桶上放一条已有规则。
	if err := c.SetLifecycleRule(LifecycleRuleOptions{ID: "keep", Prefix: strPtr("keep/"), ExpiryDays: intPtr(7)}, "mybucket"); err != nil {
		t.Fatalf("add keep: %v", err)
	}
	// upsert 相同参数: 按 ID 覆盖, 不追加第二条。
	if err := c.SetLifecycleRule(LifecycleRuleOptions{ID: "keep", Prefix: strPtr("keep/"), ExpiryDays: intPtr(30)}, "mybucket"); err != nil {
		t.Fatalf("upsert keep: %v", err)
	}
	cfg := s.config(t)
	if len(cfg.Rules) != 1 {
		t.Fatalf("after upsert: %d rules, want 1", len(cfg.Rules))
	}
	if cfg.Rules[0].Expiration == nil || cfg.Rules[0].Expiration.Days == nil || *cfg.Rules[0].Expiration.Days != 30 {
		t.Fatalf("upsert did not replace the rule: %+v", cfg.Rules[0])
	}

	// 删除唯一一条 -> 空配置不允许 PUT, 必须走 DeleteBucketLifecycle。
	before := s.deleteCount()
	if err := c.RemoveLifecycleRules(RemoveLifecycleOptions{ID: "keep"}, "mybucket"); err != nil {
		t.Fatalf("remove last rule: %v", err)
	}
	if s.deleteCount() != before+1 {
		t.Fatalf("removing the last rule must issue a config delete, deletes=%d", s.deleteCount())
	}
	if got := s.config(t); len(got.Rules) != 0 {
		t.Fatalf("config should be empty, got %v", ruleIDs(got))
	}
}

// TestRemoveLifecycleRulesSemantics: --all 必须配 --force; 不存在的 ID 报错。
func TestRemoveLifecycleRulesSemantics(t *testing.T) {
	s := newLifecycleServer(t)
	c := lifecycleAction(t, s)

	if err := c.RemoveLifecycleRules(RemoveLifecycleOptions{All: true}, "mybucket"); err == nil {
		t.Fatal("--all without --force must be rejected")
	}
	if err := c.SetLifecycle(LifecycleOptions{Prefix: "logs/", TTL: "7"}, "mybucket"); err != nil {
		t.Fatal(err)
	}
	if err := c.RemoveLifecycleRules(RemoveLifecycleOptions{ID: "missing-id"}, "mybucket"); err == nil {
		t.Fatal("removing a missing ID must fail")
	}
	if err := c.RemoveLifecycleRules(RemoveLifecycleOptions{All: true, Force: true}, "mybucket"); err != nil {
		t.Fatalf("--all --force: %v", err)
	}
	if s.deleteCount() == 0 {
		t.Fatal("--all --force must delete the configuration")
	}
}

// TestSetLifecycleFromFile: --from-file 加载 JSON 并落库。
func TestSetLifecycleFromFile(t *testing.T) {
	s := newLifecycleServer(t)
	c := lifecycleAction(t, s)

	file := filepath.Join(t.TempDir(), "lc.json")
	json := `{"Rules":[{"ID":"from-file","Status":"Enabled","Filter":{"Prefix":"tmp/"},"Expiration":{"Days":1}}]}`
	if err := os.WriteFile(file, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := c.SetLifecycle(LifecycleOptions{ConfigFile: file}, "mybucket"); err != nil {
		t.Fatalf("SetLifecycle from file: %v", err)
	}
	cfg := s.config(t)
	if len(cfg.Rules) != 1 || cfg.Rules[0].ID != "from-file" {
		t.Fatalf("stored rules = %v, want [from-file]", ruleIDs(cfg))
	}
	if !strings.Contains(s.xml, "<Status>Enabled</Status>") {
		t.Fatalf("stored xml is not a lifecycle configuration:\n%s", s.xml)
	}
}

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }
