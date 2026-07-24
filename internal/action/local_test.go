package action

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setupMpuHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir) // localMultipartStateDir / multipartStatePath 读取 $HOME
	return dir
}

func TestLocalMultipartStateDir(t *testing.T) {
	setupMpuHome(t)
	got, err := localMultipartStateDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(os.Getenv("HOME"), ".s3cli", "mpu") {
		t.Errorf("got %q", got)
	}
}

func TestListLocalMultipartStates(t *testing.T) {
	home := setupMpuHome(t)
	mpuDir := filepath.Join(home, ".s3cli", "mpu")
	os.MkdirAll(mpuDir, 0o700)

	// 目录不存在时返回空
	os.RemoveAll(mpuDir)
	states, err := ListLocalMultipartStates()
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 0 {
		t.Errorf("expected empty, got %d", len(states))
	}

	// 写入一个合法 state 文件
	os.MkdirAll(mpuDir, 0o700)
	st := multipartState{Version: 1, UploadID: "uid", Bucket: "bk", Key: "k", LocalPath: "/tmp/f", TotalSize: 100}
	data, _ := json.Marshal(st)
	p := filepath.Join(mpuDir, "abc.json")
	os.WriteFile(p, data, 0o600)
	// 非 json 扩展应跳过
	os.WriteFile(filepath.Join(mpuDir, "ignore.txt"), []byte("x"), 0o600)

	states, err = ListLocalMultipartStates()
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 {
		t.Fatalf("expected 1 state, got %d", len(states))
	}
	if states[0].UploadID != "uid" || states[0].Bucket != "bk" || states[0].StatePath != p {
		t.Errorf("bad state: %+v", states[0])
	}

	// 损坏 JSON -> error
	os.WriteFile(p, []byte("{bad"), 0o600)
	if _, err := ListLocalMultipartStates(); err == nil {
		t.Error("expected error for malformed json")
	}
}

func TestClearLocalMultipartState(t *testing.T) {
	home := setupMpuHome(t)
	mpuDir := filepath.Join(home, ".s3cli", "mpu")
	os.MkdirAll(mpuDir, 0o700)
	valid := filepath.Join(mpuDir, "x.json")
	os.WriteFile(valid, []byte("{}"), 0o600)

	// 合法 state 文件 -> 删除
	if err := ClearLocalMultipartState(valid); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(valid); !os.IsNotExist(err) {
		t.Error("file should be removed")
	}

	// 非 state 目录的文件 -> 拒绝
	outside := filepath.Join(home, "other.json")
	os.WriteFile(outside, []byte("x"), 0o600)
	if err := ClearLocalMultipartState(outside); err == nil {
		t.Error("expected error for file outside mpu dir")
	}
	// 非 .json 扩展 -> 拒绝
	insideTxt := filepath.Join(mpuDir, "x.txt")
	os.WriteFile(insideTxt, []byte("x"), 0o600)
	if err := ClearLocalMultipartState(insideTxt); err == nil {
		t.Error("expected error for non-json")
	}
}

func TestLoadMultipartState(t *testing.T) {
	setupMpuHome(t)
	mt := time.Unix(1700000000, 12345)

	// 文件不存在 -> (nil, path, nil)
	st, _, err := loadMultipartState("/tmp/myfile", "bk", "k", 100, mt)
	if err != nil || st != nil {
		t.Errorf("expected nil state for missing file, got st=%v err=%v", st, err)
	}

	// 写入匹配的 state
	path, _ := multipartStatePath("/tmp/myfile", "bk", "k")
	os.MkdirAll(filepath.Dir(path), 0o700)
	good := multipartState{Version: 1, UploadID: "uid", Bucket: "bk", Key: "k", TotalSize: 100, ModTimeUnixNs: mt.UnixNano()}
	os.WriteFile(path, mustJSON(good), 0o600)

	st, _, err = loadMultipartState("/tmp/myfile", "bk", "k", 100, mt)
	if err != nil || st == nil || st.UploadID != "uid" {
		t.Errorf("expected matching state, got st=%v err=%v", st, err)
	}

	// 字段不匹配 (size 不同) -> nil
	st, _, _ = loadMultipartState("/tmp/myfile", "bk", "k", 999, mt)
	if st != nil {
		t.Error("size mismatch should give nil")
	}

	// 损坏 JSON -> error
	os.WriteFile(path, []byte("{bad"), 0o600)
	if _, _, err := loadMultipartState("/tmp/myfile", "bk", "k", 100, mt); err == nil {
		t.Error("expected error for malformed json")
	}
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func TestMpuLocalListAndClear(t *testing.T) {
	setupMpuHome(t)
	// 空目录
	if err := MpuLocalList(MpuLocalOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := MpuLocalList(MpuLocalOptions{OutPutToJSON: true}); err != nil {
		t.Fatal(err)
	}
}

func TestParseLifecycleConfig(t *testing.T) {
	t.Run("json", func(t *testing.T) {
		data := []byte(`{"Rules":[]}`)
		c, err := parseLifecycleConfig(data, "json")
		if err != nil {
			t.Fatal(err)
		}
		if c == nil {
			t.Error("nil config")
		}
	})
	t.Run("xml", func(t *testing.T) {
		data := []byte(`<LifecycleConfiguration></LifecycleConfiguration>`)
		c, err := parseLifecycleConfig(data, "xml")
		if err != nil {
			t.Fatal(err)
		}
		if c == nil {
			t.Error("nil config")
		}
	})
	t.Run("unknown format", func(t *testing.T) {
		if _, err := parseLifecycleConfig(nil, "yaml"); err == nil {
			t.Error("expected error for unknown format")
		}
	})
	t.Run("malformed json", func(t *testing.T) {
		if _, err := parseLifecycleConfig([]byte("{bad"), "json"); err == nil {
			t.Error("expected error for malformed json")
		}
	})
}

func TestLoadJSONConfig(t *testing.T) {
	dir := t.TempDir()
	t.Run("valid json", func(t *testing.T) {
		p := filepath.Join(dir, "enc.json")
		os.WriteFile(p, []byte(`{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"AES256"}}]}`), 0o600)
		type enc struct {
			Rules []struct {
				ApplyServerSideEncryptionByDefault struct {
					SSEAlgorithm string `json:"SSEAlgorithm"`
				} `json:"ApplyServerSideEncryptionByDefault"`
			} `json:"Rules"`
		}
		cfg, err := loadJSONConfig[enc](p, "encryption")
		if err != nil {
			t.Fatal(err)
		}
		if len(cfg.Rules) != 1 || cfg.Rules[0].ApplyServerSideEncryptionByDefault.SSEAlgorithm != "AES256" {
			t.Errorf("bad config: %+v", cfg)
		}
	})
	t.Run("xml rejected", func(t *testing.T) {
		p := filepath.Join(dir, "enc.xml")
		os.WriteFile(p, []byte(`<x/>`), 0o600)
		type enc struct{}
		_, err := loadJSONConfig[enc](p, "encryption")
		if err == nil {
			t.Error("expected error for non-json")
		}
	})
	t.Run("missing file", func(t *testing.T) {
		type enc struct{}
		_, err := loadJSONConfig[enc](filepath.Join(dir, "nope.json"), "encryption")
		if err == nil {
			t.Error("expected error for missing file")
		}
	})
}

func TestListLocalDir(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "sub"), 0o700)
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("aaa"), 0o600)
	os.WriteFile(filepath.Join(root, "sub", "b.txt"), []byte("bb"), 0o600)

	entries, err := listLocalDir(root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int64{}
	for _, e := range entries {
		got[e.Path] = e.Size
	}
	if got["a.txt"] != 3 {
		t.Errorf("a.txt size = %d", got["a.txt"])
	}
	if got["sub/b.txt"] != 2 {
		t.Errorf("sub/b.txt size = %d (want 2)", got["sub/b.txt"])
	}
}

func TestStatOneFileLocal(t *testing.T) {
	dir := t.TempDir()
	e := &DiffEndpoint{IsS3: false, Path: dir}
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("hello"), 0o600)

	fe, err := statOneFile(e, "f.txt")
	if err != nil {
		t.Fatal(err)
	}
	if fe.Size != 5 {
		t.Errorf("size = %d", fe.Size)
	}
	// 目录 -> error
	os.MkdirAll(filepath.Join(dir, "sub"), 0o700)
	if _, err := statOneFile(e, "sub"); err == nil {
		t.Error("expected error for directory")
	}
	// 不存在 -> error
	if _, err := statOneFile(e, "missing"); err == nil {
		t.Error("expected error for missing")
	}
}

func TestMatchesMirrorFilters(t *testing.T) {
	// 无 include -> 全通过 (除非 exclude)
	if !matchesMirrorFilters("a/b.txt", nil, nil) {
		t.Error("no filters -> true")
	}
	// exclude 命中 -> false
	if matchesMirrorFilters("a/b.txt", nil, []string{"*.txt"}) {
		t.Error("exclude *.txt should reject")
	}
	// include 命中 -> true
	if !matchesMirrorFilters("a/b.txt", []string{"*.txt"}, nil) {
		t.Error("include *.txt should match")
	}
	// include 未命中 -> false
	if matchesMirrorFilters("a/c.csv", []string{"*.txt"}, nil) {
		t.Error("non-matching include -> false")
	}
}

func TestFilterObjects(t *testing.T) {
	in := make(chan ObjectInfo, 3)
	in <- ObjectInfo{Key: "keep.txt", Size: 1}
	in <- ObjectInfo{Key: "drop.log", Size: 2}
	in <- ObjectInfo{Key: "keep2.txt", Size: 3}
	close(in)

	out := filterObjects(in, []string{"*.txt"}, nil)
	var got []ObjectInfo
	for o := range out {
		got = append(got, o)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 filtered, got %d", len(got))
	}
	for _, o := range got {
		if o.Key == "drop.log" {
			t.Error("drop.log should be filtered out")
		}
	}
}
