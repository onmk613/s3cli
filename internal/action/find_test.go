package action

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"s3cli/pkg/api"
	"s3cli/pkg/s3iface"
)

// findTestObj 描述 mock 返回的一个对象.
type findTestObj struct {
	key          string
	size         int64
	etag         string
	storageClass string
	modified     time.Time
}

// newFindTestServer 构造返回带完整元数据的 ListObjectsV2 mock 服务端.
func newFindTestServer(t *testing.T, objs []findTestObj) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("list-type") == "2" {
			prefix := r.URL.Query().Get("prefix")
			var b strings.Builder
			b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">`)
			b.WriteString("<Name>bucket</Name><IsTruncated>false</IsTruncated>")
			for _, o := range objs {
				if !strings.HasPrefix(o.key, prefix) {
					continue
				}
				mod := o.modified
				if mod.IsZero() {
					mod = time.Now().Add(-24 * time.Hour).UTC()
				}
				fmt.Fprintf(&b, "<Contents><Key>%s</Key><LastModified>%s</LastModified><ETag>%s</ETag><Size>%d</Size><StorageClass>%s</StorageClass></Contents>",
					o.key, mod.UTC().Format("2006-01-02T15:04:05.000Z"), o.etag, o.size, o.storageClass)
			}
			b.WriteString("</ListBucketResult>")
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(b.String()))
			return
		}
		http.Error(w, "unsupported", http.StatusBadRequest)
	}))
	t.Cleanup(server.Close)
	return server
}

func newFindTestAction(t *testing.T, objs []findTestObj) *Action {
	t.Helper()
	cli, err := api.New(&api.Options{Endpoint: newFindTestServer(t, objs).URL, AccessKey: "a", SecretKey: "s"})
	if err != nil {
		t.Fatal(err)
	}
	return &Action{S3: s3iface.S3Operations(cli), Alias: "test", Ctx: context.Background()}
}

func TestFindOlderThanMatchesOldObjects(t *testing.T) {
	old := time.Now().Add(-7 * 24 * time.Hour)
	recent := time.Now().Add(-1 * time.Hour)
	c := newFindTestAction(t, []findTestObj{
		{key: "a/old.task", size: 100, etag: "e1", modified: old},
		{key: "b/recent.task", size: 200, etag: "e2", modified: recent},
	})
	out := captureStdout(t, func() {
		if err := c.FindObjects(FindOptions{OlderThan: "3d"}, "bucket", ""); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "a/old.task") {
		t.Fatalf("old object should match --older-than 3d:\n%s", out)
	}
	if strings.Contains(out, "b/recent.task") {
		t.Fatalf("recent object should not match --older-than 3d:\n%s", out)
	}
	if !strings.Contains(out, "1 matching objects") {
		t.Fatalf("summary should say 1 matching:\n%s", out)
	}
}

func TestFindOlderThanNoMatchShowsThreshold(t *testing.T) {
	c := newFindTestAction(t, []findTestObj{
		{key: "a/today.task", size: 100, etag: "e1", modified: time.Now().Add(-1 * time.Hour)},
	})
	out := captureStdout(t, func() {
		if err := c.FindObjects(FindOptions{OlderThan: "3d"}, "bucket", ""); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "no objects matched") || !strings.Contains(out, "modified before") {
		t.Fatalf("no-match output should show threshold context:\n%s", out)
	}
	if !strings.Contains(out, "out of 1 scanned") {
		t.Fatalf("no-match output should show scanned count:\n%s", out)
	}
}

func TestFindStorageClassFilter(t *testing.T) {
	c := newFindTestAction(t, []findTestObj{
		{key: "a.txt", size: 1, etag: "e1", storageClass: "STANDARD", modified: time.Now().Add(-time.Hour)},
		{key: "b.txt", size: 2, etag: "e2", storageClass: "GLACIER", modified: time.Now().Add(-time.Hour)},
	})
	out := captureStdout(t, func() {
		if err := c.FindObjects(FindOptions{StorageClass: "GLACIER"}, "bucket", ""); err != nil {
			t.Fatal(err)
		}
	})
	if strings.Contains(out, "a.txt") || !strings.Contains(out, "b.txt") {
		t.Fatalf("storage-class filter failed:\n%s", out)
	}
}

func TestFindTypeFileExcludesDirMarkers(t *testing.T) {
	c := newFindTestAction(t, []findTestObj{
		{key: "dir/", size: 0, etag: "e0", modified: time.Now().Add(-time.Hour)},
		{key: "dir/a.txt", size: 1, etag: "e1", modified: time.Now().Add(-time.Hour)},
	})
	out := captureStdout(t, func() {
		if err := c.FindObjects(FindOptions{Type: "file"}, "bucket", "dir/"); err != nil {
			t.Fatal(err)
		}
	})
	if strings.Contains(out, "DIR") || !strings.Contains(out, "dir/a.txt") {
		t.Fatalf("--type file should exclude dir markers:\n%s", out)
	}
	out = captureStdout(t, func() {
		if err := c.FindObjects(FindOptions{Type: "dir"}, "bucket", "dir/"); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "DIR") || strings.Contains(out, "dir/a.txt") {
		t.Fatalf("--type dir should only match dir markers:\n%s", out)
	}
}

func TestFindMinMaxDepth(t *testing.T) {
	c := newFindTestAction(t, []findTestObj{
		{key: "d/a.txt", size: 1, etag: "e1", modified: time.Now().Add(-time.Hour)},
		{key: "d/sub/b.txt", size: 2, etag: "e2", modified: time.Now().Add(-time.Hour)},
	})
	out := captureStdout(t, func() {
		if err := c.FindObjects(FindOptions{MinDepth: 2}, "bucket", "d/"); err != nil {
			t.Fatal(err)
		}
	})
	if strings.Contains(out, "d/a.txt") || !strings.Contains(out, "d/sub/b.txt") {
		t.Fatalf("--min-depth 2 should only match depth>=2:\n%s", out)
	}
}

func TestFindSortSize(t *testing.T) {
	c := newFindTestAction(t, []findTestObj{
		{key: "small.txt", size: 1, etag: "e1", modified: time.Now().Add(-time.Hour)},
		{key: "big.txt", size: 100, etag: "e2", modified: time.Now().Add(-time.Hour)},
		{key: "mid.txt", size: 50, etag: "e3", modified: time.Now().Add(-time.Hour)},
	})
	out := captureStdout(t, func() {
		if err := c.FindObjects(FindOptions{Sort: "-size"}, "bucket", ""); err != nil {
			t.Fatal(err)
		}
	})
	iBig := strings.Index(out, "big.txt")
	iMid := strings.Index(out, "mid.txt")
	iSmall := strings.Index(out, "small.txt")
	if iBig < 0 || iMid < 0 || iSmall < 0 || !(iBig < iMid && iMid < iSmall) {
		t.Fatalf("sort -size should be descending:\n%s", out)
	}
}

func TestFindSortTimeAndReverse(t *testing.T) {
	c := newFindTestAction(t, []findTestObj{
		{key: "older.txt", size: 1, etag: "e1", modified: time.Now().Add(-10 * time.Hour)},
		{key: "newer.txt", size: 2, etag: "e2", modified: time.Now().Add(-1 * time.Hour)},
	})
	out := captureStdout(t, func() {
		if err := c.FindObjects(FindOptions{Sort: "time"}, "bucket", ""); err != nil {
			t.Fatal(err)
		}
	})
	iOld := strings.Index(out, "older.txt")
	iNew := strings.Index(out, "newer.txt")
	if !(iOld >= 0 && iNew >= 0 && iOld < iNew) {
		t.Fatalf("sort time ascending should list older first:\n%s", out)
	}
	out = captureStdout(t, func() {
		if err := c.FindObjects(FindOptions{Sort: "time", Reverse: true}, "bucket", ""); err != nil {
			t.Fatal(err)
		}
	})
	iOld, iNew = strings.Index(out, "older.txt"), strings.Index(out, "newer.txt")
	if !(iOld >= 0 && iNew >= 0 && iNew < iOld) {
		t.Fatalf("reverse should list newer first:\n%s", out)
	}
}

func TestFindIncludeExclude(t *testing.T) {
	c := newFindTestAction(t, []findTestObj{
		{key: "logs/app.log", size: 1, etag: "e1", modified: time.Now().Add(-time.Hour)},
		{key: "logs/app.tmp", size: 2, etag: "e2", modified: time.Now().Add(-time.Hour)},
		{key: "data/app.log", size: 3, etag: "e3", modified: time.Now().Add(-time.Hour)},
	})
	out := captureStdout(t, func() {
		if err := c.FindObjects(FindOptions{Include: []string{"logs/*"}}, "bucket", ""); err != nil {
			t.Fatal(err)
		}
	})
	if strings.Contains(out, "data/app.log") || !strings.Contains(out, "logs/app.log") {
		t.Fatalf("--include filter failed:\n%s", out)
	}
	out = captureStdout(t, func() {
		if err := c.FindObjects(FindOptions{Include: []string{"logs/*"}, Exclude: []string{"*.tmp"}}, "bucket", ""); err != nil {
			t.Fatal(err)
		}
	})
	if strings.Contains(out, "app.tmp") || !strings.Contains(out, "logs/app.log") {
		t.Fatalf("--exclude filter failed:\n%s", out)
	}
}

func TestFindPrintPlaceholders(t *testing.T) {
	c := newFindTestAction(t, []findTestObj{
		{key: "a.txt", size: 42, etag: "etag-1", storageClass: "STANDARD", modified: time.Now().Add(-time.Hour)},
	})
	out := captureStdout(t, func() {
		if err := c.FindObjects(FindOptions{Print: "{name}|{size}|{etag}|{storage-class}"}, "bucket", ""); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "a.txt|42|etag-1|STANDARD") {
		t.Fatalf("--print placeholders failed:\n%s", out)
	}
}

func TestFindJSONExtraFields(t *testing.T) {
	c := newFindTestAction(t, []findTestObj{
		{key: "a.txt", size: 42, etag: "etag-1", storageClass: "STANDARD", modified: time.Now().Add(-time.Hour)},
	})
	out := captureStdout(t, func() {
		if err := c.FindObjects(FindOptions{JSON: true}, "bucket", ""); err != nil {
			t.Fatal(err)
		}
	})
	lines := jsonLines(t, out)
	if len(lines) != 1 {
		t.Fatalf("find json = %d lines\n%s", len(lines), out)
	}
	if lines[0]["etag"] != "etag-1" || lines[0]["storageClass"] != "STANDARD" || lines[0]["type"] != "file" {
		t.Errorf("find json fields = %v", lines[0])
	}
}

func TestParseFindSortErrors(t *testing.T) {
	if _, _, err := parseFindSort("bogus"); err == nil {
		t.Fatal("invalid sort should error")
	}
	field, desc, err := parseFindSort("-size")
	if err != nil || field != "size" || !desc {
		t.Fatalf("parseFindSort(-size) = %q %v %v", field, desc, err)
	}
	field, desc, err = parseFindSort("time")
	if err != nil || field != "time" || desc {
		t.Fatalf("parseFindSort(time) = %q %v %v", field, desc, err)
	}
}

func TestNormalizeFindTypeErrors(t *testing.T) {
	if _, err := normalizeFindType("blob"); err == nil {
		t.Fatal("invalid type should error")
	}
	if v, err := normalizeFindType("Directory"); err != nil || v != "dir" {
		t.Fatalf("normalizeFindType(Directory) = %q %v", v, err)
	}
}

// TestParseTimeLocalZone 验证无时区绝对时间按本地时区解析 (而非 UTC).
func TestParseTimeLocalZone(t *testing.T) {
	if time.Local.String() == "UTC" {
		t.Skip("requires non-UTC local timezone")
	}
	tm, err := parseTime("2026-08-06")
	if err != nil {
		t.Fatal(err)
	}
	utcMidnight, _ := time.Parse("2006-01-02", "2026-08-06")
	if tm.Equal(utcMidnight) {
		t.Fatalf("bare date should parse in local zone %s, not UTC: %v", time.Local, tm)
	}
	wantLocal := time.Date(2026, 8, 6, 0, 0, 0, 0, time.Local)
	if !tm.Equal(wantLocal) {
		t.Fatalf("parseTime(2026-08-06) = %v, want %v", tm, wantLocal)
	}
}

// newFindVersionsTestServer 构造支持 ListObjectVersions 的 mock 服务端.
type findVersionObj struct {
	key          string
	versionID    string
	isLatest     bool
	deleteMark   bool
	modified     time.Time
	size         int64
	etag         string
	storageClass string
}

func newFindVersionsTestAction(t *testing.T, objs []findVersionObj) *Action {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if _, ok := q["versions"]; ok {
			var b strings.Builder
			b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><ListVersionsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Name>bucket</Name><IsTruncated>false</IsTruncated>`)
			for _, o := range objs {
				mod := o.modified
				if mod.IsZero() {
					mod = time.Now().Add(-24 * time.Hour).UTC()
				}
				if o.deleteMark {
					fmt.Fprintf(&b, "<DeleteMarker><Key>%s</Key><VersionId>%s</VersionId><IsLatest>%t</IsLatest><LastModified>%s</LastModified></DeleteMarker>",
						o.key, o.versionID, o.isLatest, mod.UTC().Format("2006-01-02T15:04:05.000Z"))
				} else {
					fmt.Fprintf(&b, "<Version><Key>%s</Key><VersionId>%s</VersionId><IsLatest>%t</IsLatest><LastModified>%s</LastModified><ETag>%s</ETag><Size>%d</Size><StorageClass>%s</StorageClass></Version>",
						o.key, o.versionID, o.isLatest, mod.UTC().Format("2006-01-02T15:04:05.000Z"), o.etag, o.size, o.storageClass)
				}
			}
			b.WriteString("</ListVersionsResult>")
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(b.String()))
			return
		}
		http.Error(w, "unsupported", http.StatusBadRequest)
	}))
	t.Cleanup(server.Close)
	cli, err := api.New(&api.Options{Endpoint: server.URL, AccessKey: "a", SecretKey: "s"})
	if err != nil {
		t.Fatal(err)
	}
	return &Action{S3: s3iface.S3Operations(cli), Alias: "test", Ctx: context.Background()}
}

// TestFindVersionsOlderThanLatest 开启 versioning: 按最新版本时间过滤,
// 最新版本是 delete marker 的对象也参与匹配.
func TestFindVersionsOlderThanLatest(t *testing.T) {
	c := newFindVersionsTestAction(t, []findVersionObj{
		// old.task: 两个版本, 最新 v2 是 7 天前
		{key: "old.task", versionID: "v1", isLatest: false, modified: time.Now().Add(-10 * 24 * time.Hour), size: 100, etag: "e1", storageClass: "STANDARD"},
		{key: "old.task", versionID: "v2", isLatest: true, modified: time.Now().Add(-7 * 24 * time.Hour), size: 200, etag: "e2", storageClass: "STANDARD"},
		// recent.task: 最新版本 1 天前, 不应匹配 --older-than 3d
		{key: "recent.task", versionID: "v1", isLatest: true, modified: time.Now().Add(-1 * 24 * time.Hour), size: 50, etag: "e3", storageClass: "STANDARD"},
		// deleted.task: 最新版本是 7 天前的 delete marker
		{key: "deleted.task", versionID: "dm1", isLatest: true, deleteMark: true, modified: time.Now().Add(-7 * 24 * time.Hour)},
	})
	out := captureStdout(t, func() {
		if err := c.FindObjects(FindOptions{Versions: true, OlderThan: "3d"}, "bucket", ""); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "old.task") || !strings.Contains(out, "v2") {
		t.Fatalf("old.task latest version should match:\n%s", out)
	}
	if strings.Contains(out, "recent.task") {
		t.Fatalf("recent.task should not match --older-than 3d:\n%s", out)
	}
	if !strings.Contains(out, "deleted.task") || !strings.Contains(out, "DEL*") || !strings.Contains(out, "dm1") {
		t.Fatalf("delete-marker latest version should match:\n%s", out)
	}
}

// TestFindVersionsNoVersioningActsAsCreationTime 未开 versioning:
// 唯一版本 (null VersionId) 的时间即创建时间.
func TestFindVersionsNoVersioningActsAsCreationTime(t *testing.T) {
	c := newFindVersionsTestAction(t, []findVersionObj{
		{key: "created-7d.task", versionID: "null", isLatest: true, modified: time.Now().Add(-7 * 24 * time.Hour), size: 100, etag: "e1"},
		{key: "created-1d.task", versionID: "null", isLatest: true, modified: time.Now().Add(-1 * 24 * time.Hour), size: 200, etag: "e2"},
	})
	out := captureStdout(t, func() {
		if err := c.FindObjects(FindOptions{Versions: true, NewerThan: "3d"}, "bucket", ""); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "created-1d.task") || strings.Contains(out, "created-7d.task") {
		t.Fatalf("--versions --newer-than 3d should match only recent creation:\n%s", out)
	}
}

// TestFindVersionsJSON 验证 --versions JSON 输出含 versionId 与 isDeleteMarker.
func TestFindVersionsJSON(t *testing.T) {
	c := newFindVersionsTestAction(t, []findVersionObj{
		{key: "a.task", versionID: "v9", isLatest: true, modified: time.Now().Add(-7 * 24 * time.Hour), size: 42, etag: "e9", storageClass: "STANDARD"},
		{key: "d.task", versionID: "dm9", isLatest: true, deleteMark: true, modified: time.Now().Add(-7 * 24 * time.Hour)},
	})
	out := captureStdout(t, func() {
		if err := c.FindObjects(FindOptions{Versions: true, JSON: true}, "bucket", ""); err != nil {
			t.Fatal(err)
		}
	})
	lines := jsonLines(t, out)
	if len(lines) != 2 {
		t.Fatalf("find --versions json = %d lines\n%s", len(lines), out)
	}
	byKey := map[string]map[string]any{}
	for _, l := range lines {
		byKey[l["path"].(string)] = l
	}
	a := byKey["test:bucket/a.task"]
	if a["versionId"] != "v9" || a["type"] != "file" || a["isDeleteMarker"] != false {
		t.Errorf("version entry = %v", a)
	}
	d := byKey["test:bucket/d.task"]
	if d["versionId"] != "dm9" || d["type"] != "delete-marker" || d["isDeleteMarker"] != true {
		t.Errorf("delete-marker entry = %v", d)
	}
}
