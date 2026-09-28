package render

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"s3cli/internal/fmtutil"
)

func TestS3Path(t *testing.T) {
	cases := []struct{ alias, bucket, key, want string }{
		{"a", "b", "", "a:b"},
		{"a", "b", "k", "a:b/k"},
		{"a", "b", "d/k", "a:b/d/k"},
	}
	for _, c := range cases {
		if got := S3Path(c.alias, c.bucket, c.key); got != c.want {
			t.Errorf("S3Path(%q,%q,%q) = %q, want %q", c.alias, c.bucket, c.key, got, c.want)
		}
	}
}

func TestJSONLineEmitsOneCompactLine(t *testing.T) {
	var buf bytes.Buffer
	if err := JSONLineTo(&buf, map[string]any{"kind": "file", "size": 3}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Count(out, "\n") != 1 || !strings.HasSuffix(out, "\n") {
		t.Fatalf("want exactly one trailing newline, got %q", out)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v (%q)", err, out)
	}
	if decoded["kind"] != "file" {
		t.Errorf("kind = %v", decoded["kind"])
	}
}

func TestJSONDocIsIndented(t *testing.T) {
	var buf bytes.Buffer
	if err := JSONDocTo(&buf, map[string]any{"a": 1}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "\n  \"a\"") {
		t.Fatalf("want indented JSON with two spaces, got %q", buf.String())
	}
}

func TestVersionFlag(t *testing.T) {
	cases := []struct {
		delMarker, latest bool
		want              string
	}{
		{false, false, "VER "},
		{false, true, "VER*"},
		{true, false, "DEL "},
		{true, true, "DEL*"},
	}
	for _, c := range cases {
		if got := VersionFlag(c.delMarker, c.latest); got != c.want {
			t.Errorf("VersionFlag(%v,%v) = %q, want %q", c.delMarker, c.latest, got, c.want)
		}
	}
}

func TestVersionColor(t *testing.T) {
	if VersionColor(true) != fmtutil.Red {
		t.Error("delete markers must be red")
	}
	if VersionColor(false) != fmtutil.Green {
		t.Error("object versions must be green")
	}
}

func TestLsTableAddAndRenderDoesNotPanic(t *testing.T) {
	tbl := NewLsTable("Version ID")
	tbl.Add(LsRow{
		Time:  time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		Size:  "12",
		Type:  "VER*",
		Path:  "a:b/k",
		Extra: "v1",
		Color: fmtutil.Green,
	})
	// 零值时间必须渲染为 "-" 而不是公元 1 年。
	tbl.Add(LsRow{Size: "-", Type: "DIR", Path: "a:b/d/"})
	tbl.Render()
}

// TestTreeFilesHiddenByDefault 断言默认只显示目录, 且目录/文件的统计口径与
// 可见行一致 —— --json 的计数依赖同一份实现。
func TestTreeFilesHiddenByDefault(t *testing.T) {
	root := TreeNode{Children: []TreeNode{
		{Name: "dir", Children: []TreeNode{
			{Name: "a.txt", IsFile: true, Size: 10},
			{Name: "b.txt", IsFile: true, Size: 20},
		}},
		{Name: "top.txt", IsFile: true, Size: 5},
	}}

	dirsOnly := PrintTree(root, TreeOptions{})
	if dirsOnly.Dirs != 1 || dirsOnly.Files != 0 || dirsOnly.TotalSize != 0 {
		t.Fatalf("dirs-only: %+v", dirsOnly)
	}

	withFiles := PrintTree(root, TreeOptions{Files: true})
	if withFiles.Dirs != 1 || withFiles.Files != 3 || withFiles.TotalSize != 35 {
		t.Fatalf("with-files: %+v", withFiles)
	}
}

// TestCountTreeMatchesPrintTree 是本文件最关键的断言: 统计与渲染必须同源,
// 否则 --json 报的文件数会和文本模式看到的行数不一致。
func TestCountTreeMatchesPrintTree(t *testing.T) {
	root := TreeNode{Children: []TreeNode{
		{Name: "a", Children: []TreeNode{
			{Name: "x", Children: []TreeNode{{Name: "deep.txt", IsFile: true, Size: 1}}},
		}},
		{Name: "b.txt", IsFile: true, Size: 2},
	}}

	for _, opt := range []TreeOptions{
		{},
		{Files: true},
		{Files: true, MaxDepth: 1},
		{Files: true, MaxDepth: 2},
		{MaxDepth: 1},
	} {
		printed := PrintTree(root, opt)
		counted := CountTree(root, opt)
		if printed != counted {
			t.Errorf("opt=%+v: PrintTree=%+v CountTree=%+v (must agree)", opt, printed, counted)
		}
	}
}

// TestTreeNodeOrderDirsFirst 断言渲染顺序是"目录在前、文件在后, 同类按名"。
func TestTreeNodeOrderDirsFirst(t *testing.T) {
	n := TreeNode{Children: []TreeNode{
		{Name: "zzz.txt", IsFile: true},
		{Name: "aaa", Children: nil},
		{Name: "bbb.txt", IsFile: true},
		{Name: "ccc", Children: nil},
	}}
	got := n.sortedChildren()
	want := []string{"aaa", "ccc", "bbb.txt", "zzz.txt"}
	for i := range want {
		if got[i].Name != want[i] {
			t.Fatalf("order = %v, want %v", names(got), want)
		}
	}
}

func names(ns []TreeNode) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = n.Name
	}
	return out
}
