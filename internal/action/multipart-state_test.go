package action

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveMultipartStateUsesOwnerOnlyAtomicFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "upload.json")
	state := multipartState{Version: 1, UploadID: "upload", Bucket: "bucket", Key: "key", PartSize: minMultipartPartSize, TotalSize: 10, ModTimeUnixNs: time.Now().UnixNano()}
	if err := saveMultipartState(path, state); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("state mode = %o", info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		t.Fatalf("state data=%q err=%v", data, err)
	}
}

func TestMultipartStatePathIsStableAndDoesNotExposeSourcePath(t *testing.T) {
	one, err := multipartStatePath("/private/source/file", "bucket", "key")
	if err != nil {
		t.Fatal(err)
	}
	two, err := multipartStatePath("/private/source/file", "bucket", "key")
	if err != nil {
		t.Fatal(err)
	}
	if one != two {
		t.Fatalf("state paths differ: %q %q", one, two)
	}
	if filepath.Base(one) == "file.json" {
		t.Fatalf("state path exposes source name: %q", one)
	}
}
