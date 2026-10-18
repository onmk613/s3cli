// json_output_test.go 验证 --json 各命令输出合法且 schema 稳定
// (schema 文档: docs/OUTPUT_SCHEMA.md)。
// 基于内存 mock S3 服务端 + 自建 api 后端跑真实请求路径。

package action

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/onmk613/s3cli/internal/api"
	myprint "github.com/onmk613/s3cli/internal/fmtutil"
)

// captureStdout 捕获 fn 期间写入 os.Stdout 以及全局 myprint writer 的全部内容。
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	myprint.SetWriter(w)
	defer func() {
		os.Stdout = old
		myprint.SetWriter(old)
	}()
	fn()
	_ = w.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// captureStderr 捕获写入 stderr printer 的输出 (诊断信息通道)。
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	myprint.SetErrWriter(w)
	defer func() {
		os.Stderr = old
		myprint.SetErrWriter(old)
	}()
	fn()
	_ = w.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func newJSONTestClient(t *testing.T) *Action {
	t.Helper()
	server := httptest.NewServer(newMockS3Server())
	t.Cleanup(server.Close)
	builtin, err := api.New(&api.Options{
		Endpoint:   server.URL,
		AccessKey:  "access",
		SecretKey:  "secret",
		MaxRetries: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &Action{S3: api.S3Operations(builtin), Alias: "test", Ctx: context.Background()}
}

// jsonLines 把输出按行拆分并断言每行都是合法 JSON, 返回解码后的 map 列表。
func jsonLines(t *testing.T, out string) []map[string]any {
	t.Helper()
	var decoded []map[string]any
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("line is not valid JSON: %v\n%q", err, line)
		}
		decoded = append(decoded, m)
	}
	return decoded
}

func TestListBucketsJSON(t *testing.T) {
	c := newJSONTestClient(t)
	out := captureStdout(t, func() {
		if err := c.ListObjects(ListOptions{JSON: true}, "", ""); err != nil {
			t.Error(err)
		}
	})
	lines := jsonLines(t, out)
	if len(lines) != 1 {
		t.Fatalf("bucket list = %d lines, want 1\n%s", len(lines), out)
	}
	if lines[0]["kind"] != "bucket" || lines[0]["name"] != "mybucket" {
		t.Errorf("unexpected bucket entry: %v", lines[0])
	}
}

func TestListObjectsJSON(t *testing.T) {
	c := newJSONTestClient(t)
	out := captureStdout(t, func() {
		if err := c.ListObjects(ListOptions{JSON: true}, "mybucket", "dir/"); err != nil {
			t.Error(err)
		}
	})
	lines := jsonLines(t, out)
	// dir/sub/ (dir) + dir/a.txt + dir/b.txt (files)
	if len(lines) != 3 {
		t.Fatalf("ls = %d lines, want 3\n%s", len(lines), out)
	}
	byKind := map[string]int{}
	for _, l := range lines {
		byKind[l["kind"].(string)]++
		if l["path"] == nil {
			t.Errorf("entry missing path: %v", l)
		}
		if l["kind"] == "file" {
			if l["storageClass"] != "STANDARD" {
				t.Errorf("file entry missing storageClass: %v", l)
			}
			if l["etag"] == nil || l["etag"] == "" {
				t.Errorf("file entry missing etag: %v", l)
			}
		}
	}
	if byKind["dir"] != 1 || byKind["file"] != 2 {
		t.Errorf("kinds = %v", byKind)
	}
}

func TestListObjectsJSONSummarize(t *testing.T) {
	c := newJSONTestClient(t)
	out := captureStdout(t, func() {
		if err := c.ListObjects(ListOptions{JSON: true, Recursive: true, Summarize: true}, "mybucket", "dir/"); err != nil {
			t.Error(err)
		}
	})
	lines := jsonLines(t, out)
	last := lines[len(lines)-1]
	if last["kind"] != "summary" || last["count"].(float64) != 3 {
		t.Errorf("summary entry = %v", last)
	}
}

func TestDuObjectJSON(t *testing.T) {
	c := newJSONTestClient(t)
	out := captureStdout(t, func() {
		if err := c.DuObject(DuOptions{JSON: true}, "mybucket", "dir/"); err != nil {
			t.Error(err)
		}
	})
	lines := jsonLines(t, out)
	if len(lines) != 1 {
		t.Fatalf("du = %d lines\n%s", len(lines), out)
	}
	if lines[0]["fileNum"].(float64) != 3 || lines[0]["path"] == nil {
		t.Errorf("du entry = %v", lines[0])
	}
	if lines[0]["kind"] != "total" {
		t.Errorf("du entry kind = %v, want total", lines[0]["kind"])
	}
}

func TestFindObjectsJSON(t *testing.T) {
	c := newJSONTestClient(t)
	out := captureStdout(t, func() {
		if err := c.FindObjects(FindOptions{JSON: true}, "mybucket", "dir/"); err != nil {
			t.Error(err)
		}
	})
	lines := jsonLines(t, out)
	// 无 summary: 只输出匹配对象
	if len(lines) != 3 {
		t.Fatalf("find = %d lines, want 3\n%s", len(lines), out)
	}
	for _, l := range lines {
		if l["kind"] != nil {
			t.Errorf("find entries should have no kind: %v", l)
		}
		if l["path"] == nil || l["size"] == nil {
			t.Errorf("find entry incomplete: %v", l)
		}
	}
}

func TestTreeObjectsJSON(t *testing.T) {
	c := newJSONTestClient(t)
	out := captureStdout(t, func() {
		if err := c.TreeObjects(TreeOptions{JSON: true, Files: true}, "mybucket", "dir/"); err != nil {
			t.Error(err)
		}
	})
	lines := jsonLines(t, out)
	if len(lines) != 1 {
		t.Fatalf("tree = %d lines\n%s", len(lines), out)
	}
	doc := lines[0]
	if doc["files"].(float64) != 3 || doc["directories"].(float64) != 1 {
		t.Errorf("tree counts = %v", doc)
	}
	tree, ok := doc["tree"].(map[string]any)
	if !ok {
		t.Fatalf("tree field missing: %v", doc)
	}
	if tree["type"] != "dir" {
		t.Errorf("root type = %v", tree["type"])
	}
}

// TestTreeObjectsJSONAlwaysCountsFiles 回归: 默认 (不带 -f) 时 JSON 的
// tree 体仍包含文件, 汇总也必须统计它们, 不能出现 files:0 而树里有文件。
func TestTreeObjectsJSONAlwaysCountsFiles(t *testing.T) {
	c := newJSONTestClient(t)
	out := captureStdout(t, func() {
		if err := c.TreeObjects(TreeOptions{JSON: true}, "mybucket", "dir/"); err != nil {
			t.Error(err)
		}
	})
	lines := jsonLines(t, out)
	if len(lines) != 1 {
		t.Fatalf("tree = %d lines\n%s", len(lines), out)
	}
	if lines[0]["files"].(float64) != 3 {
		t.Errorf("tree files = %v, want 3", lines[0]["files"])
	}
	if lines[0]["totalSize"].(float64) != 31 {
		t.Errorf("tree totalSize = %v, want 31", lines[0]["totalSize"])
	}
}

func TestGetTagJSON(t *testing.T) {
	c := newJSONTestClient(t)
	out := captureStdout(t, func() {
		if err := c.GetTag(TagOptions{JSON: true}, "mybucket", ""); err != nil {
			t.Error(err)
		}
	})
	lines := jsonLines(t, out)
	if len(lines) != 1 {
		t.Fatalf("tag = %d lines\n%s", len(lines), out)
	}
	tags, ok := lines[0]["tags"].([]any)
	if !ok || len(tags) != 1 {
		t.Fatalf("tags = %v", lines[0])
	}
	tag := tags[0].(map[string]any)
	if tag["key"] != "env" || tag["value"] != "prod" {
		t.Errorf("tag entry = %v", tag)
	}
}

func TestMpuListJSON(t *testing.T) {
	c := newJSONTestClient(t)
	out := captureStdout(t, func() {
		if err := c.MpuList(MpuListOptions{JSON: true}, "mybucket", ""); err != nil {
			t.Error(err)
		}
	})
	lines := jsonLines(t, out)
	if len(lines) != 1 {
		t.Fatalf("mpu list = %d lines\n%s", len(lines), out)
	}
	if lines[0]["uploadId"] != "upload-123" || lines[0]["key"] != "big/upload.bin" {
		t.Errorf("mpu entry = %v", lines[0])
	}
}

// TestGetStdoutRejectsChecksum 流式输出无法在交付前校验整个对象,
// 必须显式拒绝而不是静默忽略 --checksum。
func TestGetStdoutRejectsChecksum(t *testing.T) {
	c := &Action{Ctx: context.Background()}
	err := c.GetObject(GetOptions{Checksum: "SHA256"}, "bucket", "key", "-")
	if err == nil || !strings.Contains(err.Error(), "--checksum") {
		t.Fatalf("want --checksum rejection, got %v", err)
	}
}
