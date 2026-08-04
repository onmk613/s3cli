// json_output_test.go 验证 --json 各命令输出合法且 schema 稳定
// (schema 文档: doc/OUTPUT_SCHEMA.md)。
// 基于内存 mock S3 服务端 + 自建 s3api 后端跑真实请求路径。

package action

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"s3cli/pkg/s3api"
	"s3cli/pkg/s3iface"
)

// captureStdout 捕获 fn 期间写入 os.Stdout 的全部内容。
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()
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
	builtin, err := s3api.New(&s3api.Options{
		Endpoint:   server.URL,
		AccessKey:  "access",
		SecretKey:  "secret",
		MaxRetries: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &Action{S3: s3iface.S3Operations(builtin), Alias: "test", Ctx: context.Background()}
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
