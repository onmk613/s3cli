package action

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func objectStream(items ...ObjectInfo) <-chan ObjectInfo {
	ch := make(chan ObjectInfo, len(items))
	for _, item := range items {
		ch <- item
	}
	close(ch)
	return ch
}

func TestStreamDiffAndKeyMapping(t *testing.T) {
	actions := make(chan diffAction, 4)
	go streamDiff(
		objectStream(ObjectInfo{Key: "a", Size: 1}, ObjectInfo{Key: "c", Size: 3}),
		objectStream(ObjectInfo{Key: "b", Size: 2}, ObjectInfo{Key: "c", Size: 3}),
		false, actions,
	)
	var got []diffAction
	for a := range actions {
		got = append(got, a)
	}
	if len(got) != 2 || got[0].rel != "a" || got[0].delete || got[1].rel != "b" || !got[1].delete {
		t.Fatalf("actions = %#v", got)
	}
	if relKey("prefix/a.txt", "prefix/") != "a.txt" || joinKey("target", "a.txt") != "target/a.txt" {
		t.Fatal("key remapping failed")
	}
}

func TestNeedsUpdate(t *testing.T) {
	now := time.Now()
	if needsUpdate(ObjectInfo{ETag: "same", Size: 1}, ObjectInfo{ETag: "same", Size: 2}) {
		t.Fatal("matching comparable etags should not update")
	}
	if !needsUpdate(ObjectInfo{ETag: "a", Size: 1}, ObjectInfo{ETag: "b", Size: 1}) {
		t.Fatal("different etags should update")
	}
	if !needsUpdate(ObjectInfo{ETag: "a-1", Size: 1, LastModified: now}, ObjectInfo{ETag: "b-1", Size: 1, LastModified: now.Add(-time.Second)}) {
		t.Fatal("newer multipart object should update")
	}
}

func TestMirrorFilters(t *testing.T) {
	if !matchesMirrorFilters("logs/a.txt", []string{"logs/*"}, nil) {
		t.Fatal("include glob should match")
	}
	if matchesMirrorFilters("logs/a.tmp", nil, []string{"*.tmp"}) {
		t.Fatal("exclude glob should win")
	}
	if matchesMirrorFilters("data/a.txt", []string{"logs/*"}, nil) {
		t.Fatal("nonmatching include should be excluded")
	}
	filtered := filterObjects(objectStream(ObjectInfo{Key: "a.txt"}, ObjectInfo{Key: "a.tmp"}), nil, []string{"*.tmp"})
	var keys []string
	for obj := range filtered {
		keys = append(keys, obj.Key)
	}
	if len(keys) != 1 || keys[0] != "a.txt" {
		t.Fatalf("filtered keys = %v", keys)
	}
}

func TestLocalDiffModes(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(a, []byte("same"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("same"), 0o600); err != nil {
		t.Fatal(err)
	}
	left := &DiffEndpoint{Path: a, Ctx: context.Background()}
	right := &DiffEndpoint{Path: b, Ctx: context.Background()}
	if err := Diff(DiffOptions{A: left, B: right, Mode: DiffModeMD5}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("diff"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Diff(DiffOptions{A: left, B: right, Mode: DiffModeMD5}); !errors.Is(err, errDiffer) {
		t.Fatalf("Diff error = %v", err)
	}
}
