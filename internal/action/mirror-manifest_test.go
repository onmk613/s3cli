package action

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMirrorManifestAppendAndResume(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "mirror.manifest")
	m, err := openMirrorManifest(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.mark("first"); err != nil {
		t.Fatal(err)
	}
	if err := m.mark("first"); err != nil {
		t.Fatal(err)
	}
	if err := m.mark("nested/second"); err != nil {
		t.Fatal(err)
	}
	if err := m.close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "first\nnested/second\n" {
		t.Fatalf("manifest=%q err=%v", data, err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}
	resumed, err := openMirrorManifest(path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.close()
	if !resumed.has("first") || !resumed.has("nested/second") || resumed.has("missing") {
		t.Fatal("resume state incorrect")
	}
}

func TestMirrorResumeRequiresManifest(t *testing.T) {
	if _, err := openMirrorManifest("", true); err == nil {
		t.Fatal("expected manifest requirement")
	}
}
