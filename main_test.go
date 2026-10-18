// main_test.go 是进程级的 CLI 契约测试。
//
// 包内单测无法覆盖 main -> cobra -> os.Exit 这条真实路径, 也看不到
// stdout/stderr 的分离与真实退出码。这里构建一次二进制, 用假 S3 服务端
// 跑关键命令, 专门锁定这些"用户/脚本能看到"的契约:
//   - 退出码 0/1/4;
//   - stdout 只放数据, 错误/标题放 stderr;
//   - --json 输出必须是合法 JSON;
//   - README 里承诺的语法 (ls alias:) 真的可用。

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var cliPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "s3cli-contract-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "mktemp:", err)
		os.Exit(1)
	}
	cliPath = filepath.Join(dir, "s3cli")
	if runtime.GOOS == "windows" {
		cliPath += ".exe"
	}
	build := exec.Command("go", "build", "-trimpath", "-o", cliPath, ".")
	build.Stdout = os.Stderr
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "build s3cli:", err)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// fakeS3 提供契约测试所需的最小 S3 行为。
func fakeS3(t *testing.T) *httptest.Server {
	t.Helper()
	writeXML := func(w http.ResponseWriter, body string) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(body))
	}
	notFound := func(w http.ResponseWriter, code string) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprintf(w, `<Error><Code>%s</Code><Message>%s</Message></Error>`, code, code)
	}
	listBody := func(prefix, delimiter string) string {
		if prefix != "" {
			return `<ListBucketResult><Name>demo</Name><Prefix>` + prefix + `</Prefix><KeyCount>0</KeyCount><MaxKeys>1000</MaxKeys><IsTruncated>false</IsTruncated></ListBucketResult>`
		}
		head := `<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Name>demo</Name><Prefix></Prefix><MaxKeys>1000</MaxKeys><IsTruncated>false</IsTruncated>
  <Contents><Key>a.txt</Key><LastModified>2026-09-01T10:00:00.000Z</LastModified><ETag>&quot;etag-a&quot;</ETag><Size>12</Size><StorageClass>STANDARD</StorageClass></Contents>`
		if delimiter == "/" {
			return head + `<KeyCount>1</KeyCount><CommonPrefixes><Prefix>dir/</Prefix></CommonPrefixes></ListBucketResult>`
		}
		return head + `<KeyCount>2</KeyCount><Contents><Key>dir/b.txt</Key><LastModified>2026-09-02T10:00:00.000Z</LastModified><ETag>&quot;etag-b&quot;</ETag><Size>34</Size><StorageClass>STANDARD</StorageClass></Contents></ListBucketResult>`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case r.URL.Path == "/" || r.URL.Path == "":
			writeXML(w, `<ListAllMyBucketsResult><Buckets><Bucket><Name>demo</Name><CreationDate>2026-01-01T00:00:00.000Z</CreationDate></Bucket></Buckets></ListAllMyBucketsResult>`)
		case strings.HasPrefix(r.URL.Path, "/demo/") && r.Method == http.MethodHead:
			if strings.HasSuffix(r.URL.Path, "/a.txt") {
				w.Header().Set("Content-Length", "12")
				w.Header().Set("ETag", `"etag-a"`)
				w.WriteHeader(http.StatusOK)
				return
			}
			notFound(w, "NoSuchKey")
		case r.URL.Path != "/demo":
			w.WriteHeader(http.StatusNotFound)
		case q.Has("location"):
			writeXML(w, `<LocationConstraint>us-east-1</LocationConstraint>`)
		case q.Has("versioning"):
			writeXML(w, `<VersioningConfiguration/>`)
		case q.Has("policy"):
			notFound(w, "NoSuchBucketPolicy")
		case q.Has("cors"):
			notFound(w, "NoSuchCORSConfiguration")
		case q.Has("tagging"):
			notFound(w, "NoSuchTagSet")
		case q.Has("lifecycle"):
			writeXML(w, `<LifecycleConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Rule><ID>r1</ID><Filter><Prefix>logs/</Prefix></Filter><Status>Enabled</Status><Expiration><Days>30</Days></Expiration></Rule></LifecycleConfiguration>`)
		case r.Method == http.MethodGet && q.Get("list-type") == "2":
			writeXML(w, listBody(q.Get("prefix"), q.Get("delimiter")))
		case r.Method == http.MethodGet && q.Has("acl"):
			writeXML(w, `<AccessControlPolicy><Owner><ID>owner</ID></Owner><AccessControlList></AccessControlList></AccessControlPolicy>`)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func writeConfig(t *testing.T, endpoint string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s3cli.conf")
	body := fmt.Sprintf("[t]\nhost_base = %q\naccess_key = \"access\"\nsecret_key = \"secret\"\nregion = \"us-east-1\"\n", endpoint)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func cliEnv(home string) []string {
	env := make([]string, 0, len(os.Environ())+4)
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "HOME=") || strings.HasPrefix(e, "USERPROFILE=") ||
			strings.HasPrefix(e, "CLI_LANG=") || strings.HasPrefix(e, "NO_COLOR=") {
			continue
		}
		env = append(env, e)
	}
	return append(env, "HOME="+home, "USERPROFILE="+home, "CLI_LANG=en", "NO_COLOR=1")
}

type cliResult struct {
	stdout string
	stderr string
	code   int
}

func runCLI(t *testing.T, home string, args ...string) cliResult {
	t.Helper()
	cmd := exec.Command(cliPath, args...)
	cmd.Env = cliEnv(home)
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	code := 0
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("run %v: %v", args, err)
		}
		code = exitErr.ExitCode()
	}
	return cliResult{stdout: out.String(), stderr: errb.String(), code: code}
}

func parseJSONDoc(t *testing.T, s string) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(s), &doc); err != nil {
		t.Fatalf("stdout is not a JSON document: %v\n%s", err, s)
	}
	return doc
}

func TestCLIListsBucketsWithAliasColon(t *testing.T) {
	srv := fakeS3(t)
	conf := writeConfig(t, srv.URL)
	home := t.TempDir()

	got := runCLI(t, home, "-f", conf, "ls", "t:")
	if got.code != 0 {
		t.Fatalf("exit = %d, stderr=%s", got.code, got.stderr)
	}
	if !strings.Contains(got.stdout, "demo") {
		t.Fatalf("stdout does not list bucket: %q", got.stdout)
	}
	if got.stderr != "" {
		t.Fatalf("stderr should be empty, got %q", got.stderr)
	}
}

func TestCLIJSONIsClean(t *testing.T) {
	srv := fakeS3(t)
	conf := writeConfig(t, srv.URL)

	got := runCLI(t, t.TempDir(), "-f", conf, "ls", "t:demo", "--json")
	if got.code != 0 {
		t.Fatalf("exit = %d, stderr=%s", got.code, got.stderr)
	}
	lines := strings.Split(strings.TrimSpace(got.stdout), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2: %q", len(lines), got.stdout)
	}
	for _, line := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("invalid JSON line: %v\n%s", err, line)
		}
		if m["path"] == nil {
			t.Fatalf("missing path: %v", m)
		}
		if m["kind"] == "file" && (m["storageClass"] == nil || m["etag"] == nil) {
			t.Fatalf("file entry missing storageClass/etag: %v", m)
		}
	}
	if got.stderr != "" {
		t.Fatalf("stderr should be empty, got %q", got.stderr)
	}
}

func TestCLIInfoIsPureJSON(t *testing.T) {
	srv := fakeS3(t)
	conf := writeConfig(t, srv.URL)

	got := runCLI(t, t.TempDir(), "-f", conf, "info", "t:demo")
	if got.code != 0 {
		t.Fatalf("exit = %d, stderr=%s", got.code, got.stderr)
	}
	if strings.Contains(got.stdout, "#") {
		t.Fatalf("info stdout must be pure JSON, got %q", got.stdout)
	}
	doc := parseJSONDoc(t, got.stdout)
	if doc["Bucket"] != "demo" {
		t.Fatalf("Bucket = %v", doc["Bucket"])
	}
	if !strings.Contains(got.stderr, "info(bucket)") {
		t.Fatalf("human header should go to stderr, got %q", got.stderr)
	}
}

func TestCLILifecycleJSONIsPure(t *testing.T) {
	srv := fakeS3(t)
	conf := writeConfig(t, srv.URL)

	got := runCLI(t, t.TempDir(), "-f", conf, "bucket", "lifecycle", "list", "t:demo", "--json")
	if got.code != 0 {
		t.Fatalf("exit = %d, stderr=%s", got.code, got.stderr)
	}
	doc := parseJSONDoc(t, got.stdout)
	if doc["Rules"] == nil {
		t.Fatalf("Rules missing: %v", doc)
	}
	// JSON 模式不再输出人类可读标题; stderr 保持干净。
	if got.stderr != "" {
		t.Fatalf("stderr should be empty in JSON mode, got %q", got.stderr)
	}
}

func TestCLIErrorsGoToStderr(t *testing.T) {
	srv := fakeS3(t)
	conf := writeConfig(t, srv.URL)

	got := runCLI(t, t.TempDir(), "-f", conf, "stat", "t:demo/nope.txt")
	if got.code != 4 {
		t.Fatalf("exit = %d, want 4; stderr=%s", got.code, got.stderr)
	}
	if got.stdout != "" {
		t.Fatalf("stdout must stay empty on error, got %q", got.stdout)
	}
	if !strings.Contains(got.stderr, "does not exist") && !strings.Contains(got.stderr, "不存在") {
		t.Fatalf("stderr missing error: %q", got.stderr)
	}
}

func TestCLITagListEmptyIsSuccess(t *testing.T) {
	srv := fakeS3(t)
	conf := writeConfig(t, srv.URL)

	got := runCLI(t, t.TempDir(), "-f", conf, "tag", "list", "t:demo")
	if got.code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%s", got.code, got.stderr)
	}
	if !strings.Contains(got.stdout, "no tags") && !strings.Contains(got.stdout, "无标签") {
		t.Fatalf("expected empty-tag output, got %q", got.stdout)
	}
}

func TestCLITreeJSONCountsFiles(t *testing.T) {
	srv := fakeS3(t)
	conf := writeConfig(t, srv.URL)

	got := runCLI(t, t.TempDir(), "-f", conf, "tree", "t:demo", "--json")
	if got.code != 0 {
		t.Fatalf("exit = %d, stderr=%s", got.code, got.stderr)
	}
	doc := parseJSONDoc(t, got.stdout)
	if doc["files"].(float64) != 2 || doc["totalSize"].(float64) != 46 {
		t.Fatalf("tree stats = files:%v totalSize:%v", doc["files"], doc["totalSize"])
	}
}

func TestCLILocalDiffWithoutConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := runCLI(t, t.TempDir(), "diff", dir, dir, "--json")
	if got.code != 0 {
		t.Fatalf("exit = %d, stderr=%s", got.code, got.stderr)
	}
	parseJSONDoc(t, got.stdout)
}

func TestCLIMirrorRemoveRequiresForce(t *testing.T) {
	srv := fakeS3(t)
	conf := writeConfig(t, srv.URL)

	got := runCLI(t, t.TempDir(), "-f", conf, "mirror", "t:demo", "t:demo2", "--remove")
	if got.code == 0 {
		t.Fatalf("mirror --remove without --force must fail, stdout=%q", got.stdout)
	}
	if got.stdout != "" {
		t.Fatalf("stdout must stay empty, got %q", got.stdout)
	}
	if !strings.Contains(got.stderr, "--force") {
		t.Fatalf("stderr missing --force hint: %q", got.stderr)
	}
}

func TestCLIGetStdoutChecksumRejected(t *testing.T) {
	srv := fakeS3(t)
	conf := writeConfig(t, srv.URL)

	got := runCLI(t, t.TempDir(), "-f", conf, "get", "t:demo/a.txt", "-", "--checksum", "SHA256")
	if got.code != 1 {
		t.Fatalf("exit = %d, want 1; stderr=%s", got.code, got.stderr)
	}
	if got.stdout != "" {
		t.Fatalf("stdout must stay empty, got %q", got.stdout)
	}
	if !strings.Contains(got.stderr, "--checksum") {
		t.Fatalf("stderr missing --checksum hint: %q", got.stderr)
	}
}

func TestCLIChineseErrorPrefixIsLocalized(t *testing.T) {
	srv := fakeS3(t)
	conf := writeConfig(t, srv.URL)

	got := runCLI(t, t.TempDir(), "-f", conf, "--lang", "zh", "stat", "t:demo/nope.txt")
	if got.code != 4 {
		t.Fatalf("exit = %d, want 4; stderr=%s", got.code, got.stderr)
	}
	if !strings.Contains(got.stderr, "检查 S3 路径") {
		t.Fatalf("Chinese error prefix missing: %q", got.stderr)
	}
	if strings.Contains(got.stderr, "check s3 path:") {
		t.Fatalf("English prefix leaked into Chinese output: %q", got.stderr)
	}
}

func TestCLIAliasOnlyRejectedForNonLs(t *testing.T) {
	srv := fakeS3(t)
	conf := writeConfig(t, srv.URL)

	got := runCLI(t, t.TempDir(), "-f", conf, "stat", "t:")
	if got.code == 0 {
		t.Fatalf("stat alias-only must fail, stdout=%q", got.stdout)
	}
	if got.stdout != "" {
		t.Fatalf("stdout must stay empty, got %q", got.stdout)
	}
	if !strings.Contains(got.stderr, "bucket") {
		t.Fatalf("stderr missing bucket hint: %q", got.stderr)
	}
}

func TestCLIVersion(t *testing.T) {
	got := runCLI(t, t.TempDir(), "--version")
	if got.code != 0 {
		t.Fatalf("exit = %d", got.code)
	}
	if !strings.HasPrefix(got.stdout, "s3cli version ") {
		t.Fatalf("unexpected version output: %q", got.stdout)
	}
}
