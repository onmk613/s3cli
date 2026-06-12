package utils

import "testing"

// rel 固定为 file1.txt，源为 path1/to1[/]，验证用户给定的 6 条规则。
//
// 源对象示例: bucket1/path1/to1/file1.txt
// 这里只验证 key 计算（不含 bucket/alias），rel = file1.txt。

func resolveDirKey(srcKey string, srcTrailing bool, destKey string, destTrailing bool, state DestState, rel string) string {
	prefix, appendRel := ResolveDirDestPrefix(srcKey, srcTrailing, destKey, destTrailing, state)
	if !appendRel {
		return prefix
	}
	if prefix == "" {
		return rel
	}
	return prefix + "/" + rel
}

func TestResolveDirDestPrefix_Rules1to4(t *testing.T) {
	const rel = "file1.txt"
	cases := []struct {
		name         string
		srcKey       string
		srcTrailing  bool
		destKey      string
		destTrailing bool
		state        DestState
		want         string
	}{
		// 规则1: path1/to1/  ->  path2/to2 (no slash)
		{"r1-none", "path1/to1/", true, "path2/to2", false, DestNone, "path2/to2"},
		{"r1-dir", "path1/to1/", true, "path2/to2", false, DestDir, "path2/to2/file1.txt"},
		{"r1-file", "path1/to1/", true, "path2/to2", false, DestFile, "path2/to2"},

		// 规则2: path1/to1/  ->  path2/to2/ (slash)
		{"r2-none", "path1/to1/", true, "path2/to2/", true, DestNone, "path2/to2/file1.txt"},
		{"r2-dir", "path1/to1/", true, "path2/to2/", true, DestDir, "path2/to2/file1.txt"},
		{"r2-file", "path1/to1/", true, "path2/to2/", true, DestFile, "path2/to2/file1.txt"},

		// 规则3: path1/to1 (no slash)  ->  path2/to2 (no slash)
		{"r3-none", "path1/to1", false, "path2/to2", false, DestNone, "path2/to2/file1.txt"},
		{"r3-dir", "path1/to1", false, "path2/to2", false, DestDir, "path2/to2/file1.txt"},
		{"r3-file", "path1/to1", false, "path2/to2", false, DestFile, "path2/to2"},

		// 规则4: path1/to1 (no slash)  ->  path2/to2/ (slash)
		{"r4-none", "path1/to1", false, "path2/to2/", true, DestNone, "path2/to2/to1/file1.txt"},
		{"r4-dir", "path1/to1", false, "path2/to2/", true, DestDir, "path2/to2/to1/file1.txt"},
		{"r4-file", "path1/to1", false, "path2/to2/", true, DestFile, "path2/to2/to1/file1.txt"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := resolveDirKey(c.srcKey, c.srcTrailing, c.destKey, c.destTrailing, c.state, rel)
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestResolveFileDest_Rules5to6(t *testing.T) {
	const base = "file1.txt"
	cases := []struct {
		name         string
		destKey      string
		destTrailing bool
		want         string
	}{
		// 规则5: path1/to1/file1.txt -> path2/to2 (no slash)
		{"r5", "path2/to2", false, "path2/to2"},
		// 规则6: path1/to1/file1.txt -> path2/to2/ (slash)
		{"r6", "path2/to2/", true, "path2/to2/file1.txt"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ResolveFileDest(c.destKey, c.destTrailing, base)
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}
