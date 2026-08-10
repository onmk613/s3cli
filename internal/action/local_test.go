package action

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"s3cli/pkg/s3iface"
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
	if err := MpuLocalList(MpuLocalOptions{OutputToJSON: true}); err != nil {
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

func TestParseTTLDays(t *testing.T) {
	cases := []struct {
		in      string
		want    int
		wantErr bool
	}{
		{"30", 30, false},
		{"30d", 30, false},
		{"30days", 30, false},
		{"12h", 1, false}, // ceil(0.5) = 1
		{"25h", 2, false}, // ceil(25/24) = 2
		{"1w", 7, false},
		{"2weeks", 14, false},
		{"1m", 30, false},
		{"1y", 365, false},
		{"0", 0, true},   // 非正数
		{"-5", 0, true},  // 非正数
		{"abc", 0, true}, // 非数字
		{"12x", 0, true}, // 未知单位
		{"", 0, true},    // 空
	}
	for _, tc := range cases {
		got, err := ParseTTLDays(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseTTLDays(%q) = %d, want error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseTTLDays(%q) unexpected error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseTTLDays(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
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

func TestParseByteSize(t *testing.T) {
	cases := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"1048576", 1048576, false},
		{"1MiB", 1 << 20, false},
		{"1mib", 1 << 20, false},
		{"1MB", 1 << 20, false},
		{"1K", 1 << 10, false},
		{"10k", 10 << 10, false},
		{"1.5G", 1.5 * (1 << 30), false},
		{"1GiB", 1 << 30, false},
		{"1T", 1 << 40, false},
		{"2TiB", 2 << 40, false},
		{"512B", 512, false},
		{"", 0, true},
		{"abc", 0, true},
		{"1XB", 0, true},
		{"-5", 0, true},
		{"0", 0, true},
	}
	for _, tc := range cases {
		got, err := ParseByteSize(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseByteSize(%q) = %d, want error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseByteSize(%q) unexpected error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseByteSize(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestParseILMTags(t *testing.T) {
	tags := parseILMTags("k1=v1&k2=v2")
	if len(tags) != 2 || tags[0].Key != "k1" || tags[0].Value != "v1" || tags[1].Key != "k2" || tags[1].Value != "v2" {
		t.Errorf("bad tags: %+v", tags)
	}
	// 无 '=' 项: 仅 Key
	only := parseILMTags("bare&a=b")
	if len(only) != 2 || only[0].Key != "bare" || only[0].Value != "" {
		t.Errorf("bad tags: %+v", only)
	}
	// 空串
	if len(parseILMTags("")) != 0 {
		t.Error("empty tags should give empty list")
	}
}

func TestBuildLifecycleRule(t *testing.T) {
	intPtr := func(v int) *int { return &v }
	boolPtr := func(v bool) *bool { return &v }
	strPtr := func(v string) *string { return &v }

	t.Run("expire-days only", func(t *testing.T) {
		rule, err := buildLifecycleRule(LifecycleRuleOptions{ID: "r1", ExpiryDays: intPtr(30)})
		if err != nil {
			t.Fatal(err)
		}
		if rule.ID != "r1" || rule.Status != "Enabled" || rule.Expiration == nil || *rule.Expiration.Days != 30 {
			t.Errorf("bad rule: %+v", rule)
		}
	})

	t.Run("auto id", func(t *testing.T) {
		rule, err := buildLifecycleRule(LifecycleRuleOptions{ExpiryDays: intPtr(30), Prefix: strPtr("logs/")})
		if err != nil || rule.ID == "" {
			t.Fatalf("expected auto id, got %+v err=%v", rule, err)
		}
		// 确定性: 相同内容得到相同 ID, 内容不同 (天数/动作) 得到不同 ID.
		rule2, err := buildLifecycleRule(LifecycleRuleOptions{ExpiryDays: intPtr(30), Prefix: strPtr("logs/")})
		if err != nil {
			t.Fatal(err)
		}
		if rule.ID != rule2.ID {
			t.Errorf("same content must derive same ID: %q vs %q", rule.ID, rule2.ID)
		}
		rule3, err := buildLifecycleRule(LifecycleRuleOptions{ExpiryDays: intPtr(90), Prefix: strPtr("logs/")})
		if err != nil {
			t.Fatal(err)
		}
		if rule.ID == rule3.ID {
			t.Errorf("different days must derive different IDs, both %q", rule.ID)
		}
		rule4, err := buildLifecycleRule(LifecycleRuleOptions{Prefix: strPtr("logs/"), TransitionDays: intPtr(30), TransitionTier: strPtr("GLACIER")})
		if err != nil {
			t.Fatal(err)
		}
		if rule.ID == rule4.ID {
			t.Errorf("different actions must derive different IDs, both %q", rule.ID)
		}
	})

	t.Run("filter with prefix+size -> And", func(t *testing.T) {
		lt := int64(1 << 20)
		rule, err := buildLifecycleRule(LifecycleRuleOptions{ID: "r2", Prefix: strPtr("doc/"), SizeLT: &lt, ExpiryDays: intPtr(90)})
		if err != nil {
			t.Fatal(err)
		}
		if rule.Filter == nil || rule.Filter.And == nil || rule.Filter.And.Prefix != "doc/" || *rule.Filter.And.ObjectSizeLessThan != lt {
			t.Errorf("bad filter: %+v", rule.Filter)
		}
	})

	t.Run("single tag filter", func(t *testing.T) {
		rule, err := buildLifecycleRule(LifecycleRuleOptions{ID: "r3", Tags: strPtr("env=prod"), ExpiryDays: intPtr(7)})
		if err != nil {
			t.Fatal(err)
		}
		if rule.Filter == nil || rule.Filter.Tag == nil || rule.Filter.Tag.Key != "env" {
			t.Errorf("bad tag filter: %+v", rule.Filter)
		}
	})

	t.Run("transition requires tier", func(t *testing.T) {
		if _, err := buildLifecycleRule(LifecycleRuleOptions{ID: "r4", TransitionDays: intPtr(30)}); err == nil {
			t.Error("expected error: transition tier missing")
		}
	})

	t.Run("tier requires days", func(t *testing.T) {
		if _, err := buildLifecycleRule(LifecycleRuleOptions{ID: "r5", TransitionTier: strPtr("GLACIER")}); err == nil {
			t.Error("expected error: transition days missing")
		}
	})

	t.Run("no action", func(t *testing.T) {
		if _, err := buildLifecycleRule(LifecycleRuleOptions{ID: "r6", Prefix: strPtr("x/")}); err == nil {
			t.Error("expected error: no action")
		}
	})

	t.Run("expire-delete-marker and expire-days conflict", func(t *testing.T) {
		if _, err := buildLifecycleRule(LifecycleRuleOptions{ID: "r7", ExpiryDays: intPtr(1), ExpireDeleteMarker: boolPtr(true)}); err == nil {
			t.Error("expected error: conflicting expiry options")
		}
	})

	t.Run("full rule", func(t *testing.T) {
		rule, err := buildLifecycleRule(LifecycleRuleOptions{
			ID: "r8", ExpiryDays: intPtr(200),
			TransitionDays: intPtr(90), TransitionTier: strPtr("glacier"),
			NoncurrentExpireDays: intPtr(100), NoncurrentExpireNewer: intPtr(5),
			NoncurrentTransitionDays: intPtr(45), NoncurrentTransitionTier: strPtr("GLACIER-IR"),
		})
		if err != nil {
			t.Fatal(err)
		}
		if rule.Transitions[0].StorageClass != "GLACIER" {
			t.Errorf("transition storage class = %s", rule.Transitions[0].StorageClass)
		}
		if *rule.NoncurrentVersionExpiration.NewerNoncurrentVersions != 5 {
			t.Errorf("newer noncurrent = %v", rule.NoncurrentVersionExpiration.NewerNoncurrentVersions)
		}
		if rule.NoncurrentVersionTransitions[0].StorageClass != "GLACIER-IR" {
			t.Errorf("noncurrent transition storage class = %s", rule.NoncurrentVersionTransitions[0].StorageClass)
		}
	})

	t.Run("invalid date", func(t *testing.T) {
		if _, err := buildLifecycleRule(LifecycleRuleOptions{ID: "r9", ExpiryDate: strPtr("2026/01/01")}); err == nil {
			t.Error("expected error: invalid date")
		}
	})
}

func TestParseDuration(t *testing.T) {
	cases := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"7d", 7 * 24 * time.Hour, false},
		{"7d10h31s", 7*24*time.Hour + 10*time.Hour + 31*time.Second, false},
		{"30m", 30 * time.Minute, false},
		{"90s", 90 * time.Second, false},
		{"1h30m", 90 * time.Minute, false},
		{"", 0, true},
		{"10x", 0, true},
		{"abc", 0, true},
		{"1d2", 0, true},
	}
	for _, tc := range cases {
		got, err := ParseDuration(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseDuration(%q) = %v, want error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseDuration(%q) unexpected error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseDuration(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestParseFilterTime(t *testing.T) {
	// 时长 -> 过去的时间点
	ts, err := parseFilterTime("1h")
	if err != nil {
		t.Fatal(err)
	}
	if diff := time.Since(ts); diff < 59*time.Minute || diff > 61*time.Minute {
		t.Errorf("parseFilterTime(1h) off: %v", diff)
	}
	// 绝对时间
	ts, err = parseFilterTime("2026-01-02")
	if err != nil {
		t.Fatal(err)
	}
	if ts.Year() != 2026 || ts.Month() != 1 || ts.Day() != 2 {
		t.Errorf("bad absolute time: %v", ts)
	}
	// 空 -> zero
	ts, err = parseFilterTime("")
	if err != nil || !ts.IsZero() {
		t.Errorf("empty should give zero time, got %v err=%v", ts, err)
	}
	// 非法
	if _, err := parseFilterTime("bogus"); err == nil {
		t.Error("expected error")
	}
}

func TestParseSelectOpts(t *testing.T) {
	kv, err := parseSelectOpts("rd=\\n,fh=USE,fd=;", validSelectKeys)
	if err != nil {
		t.Fatal(err)
	}
	if kv["RecordDelimiter"] != "\n" || kv["FileHeader"] != "USE" || kv["FieldDelimiter"] != ";" {
		t.Errorf("bad parse: %v", kv)
	}
	// 长名
	kv, err = parseSelectOpts("recorddelimiter=\\r\\n,comments=#", validSelectKeys)
	if err != nil {
		t.Fatal(err)
	}
	if kv["RecordDelimiter"] != "\r\n" || kv["Comments"] != "#" {
		t.Errorf("bad parse: %v", kv)
	}
	// 非法键
	if _, err := parseSelectOpts("bogus=1", validSelectKeys); err == nil {
		t.Error("expected error for invalid key")
	}
	// 缺少 =
	if _, err := parseSelectOpts("novalue", validSelectKeys); err == nil {
		t.Error("expected error for key without value")
	}
	// 重复键
	if _, err := parseSelectOpts("rd=a,rd=b", validSelectKeys); err == nil {
		t.Error("expected error for duplicate key")
	}
}

func TestBuildSelectSerializations(t *testing.T) {
	// 默认 CSV + 首行为表头 + NONE 压缩
	in, out, err := buildSelectSerializations(SelectOptions{}, "data.csv")
	if err != nil {
		t.Fatal(err)
	}
	if in.Format != "CSV" || in.FileHeaderInfo != "USE" || in.CompressionType != "NONE" {
		t.Errorf("bad default input: %+v", in)
	}
	if out.Format != "CSV" || out.RecordDelimiter != "\n" {
		t.Errorf("bad default output: %+v", out)
	}

	// .gz 自动识别压缩
	in, _, err = buildSelectSerializations(SelectOptions{}, "data.csv.gz")
	if err != nil || in.CompressionType != "GZIP" {
		t.Errorf("gz detection: %+v err=%v", in, err)
	}
	// .json 自动识别格式
	in, _, err = buildSelectSerializations(SelectOptions{}, "data.json")
	if err != nil || in.Format != "JSON" || in.JSONType != "LINES" {
		t.Errorf("json detection: %+v err=%v", in, err)
	}
	// .parquet
	in, _, err = buildSelectSerializations(SelectOptions{}, "data.parquet")
	if err != nil || in.Format != "PARQUET" {
		t.Errorf("parquet detection: %+v err=%v", in, err)
	}
	// 显式 csv-input
	in, _, err = buildSelectSerializations(SelectOptions{CSVInput: "rd=\\n,fh=USE,fd=;"}, "data.csv")
	if err != nil || in.FieldDelimiter != ";" || in.FileHeaderInfo != "USE" {
		t.Errorf("csv-input: %+v err=%v", in, err)
	}
	// 冲突
	if _, _, err := buildSelectSerializations(SelectOptions{CSVInput: "rd=a", JSONInput: "type=LINES"}, "x"); err == nil {
		t.Error("expected error for conflicting input formats")
	}
	if _, _, err := buildSelectSerializations(SelectOptions{CSVOutput: "rd=a", JSONOutput: "rd=b"}, "x"); err == nil {
		t.Error("expected error for conflicting output formats")
	}
	// json-output
	_, out, err = buildSelectSerializations(SelectOptions{JSONOutput: "rd=\\n"}, "data.csv")
	if err != nil || out.Format != "JSON" {
		t.Errorf("json-output: %+v err=%v", out, err)
	}
	// 显式 compression
	in, _, err = buildSelectSerializations(SelectOptions{Compression: "BZIP2"}, "data.csv")
	if err != nil || in.CompressionType != "BZIP2" {
		t.Errorf("compression: %+v err=%v", in, err)
	}
}

func TestStatMetadata(t *testing.T) {
	head := &s3iface.HeadObjectOutput{
		ContentType:          "text/plain",
		ContentEncoding:      "gzip",
		ContentDisposition:   "inline",
		ContentLanguage:      "en",
		CacheControl:         "no-cache",
		StorageClass:         "STANDARD_IA",
		ServerSideEncryption: "AES256",
		SSEKMSKeyID:          "kms-key",
		ObjectLockMode:       "GOVERNANCE",
		Metadata:             map[string]string{"foo": "bar"},
	}
	meta := statMetadata(head)
	if meta["Content-Type"] != "text/plain" || meta["Content-Encoding"] != "gzip" {
		t.Errorf("basic metadata missing: %v", meta)
	}
	if meta["x-amz-storage-class"] != "STANDARD_IA" {
		t.Errorf("storage class missing: %v", meta)
	}
	if meta["x-amz-meta-foo"] != "bar" {
		t.Errorf("user metadata missing: %v", meta)
	}
	// 空 head 不应 panic, 返回空 map
	if len(statMetadata(&s3iface.HeadObjectOutput{})) != 0 {
		t.Error("empty head should give empty metadata")
	}
}

func TestPathBase(t *testing.T) {
	cases := map[string]string{
		"a/b/c.txt": "c.txt",
		"plain":     "plain",
		"a/b/":      "",
		"":          "",
	}
	for in, want := range cases {
		if got := pathBase(in); got != want {
			t.Errorf("pathBase(%q) = %q, want %q", in, got, want)
		}
	}
}
