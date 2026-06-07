package utils

import "testing"

func TestResolveDestKey(t *testing.T) {
	tests := []struct {
		name     string
		destPath string
		destKey  string
		srcBase  string
		want     string
	}{
		{"dest is dir, key not empty", "a/b/", "dst", "file.txt", "dst/file.txt"},
		{"dest is dir, key already has slash", "a/b/", "dst/", "file.txt", "dst/file.txt"},
		{"dest is dir, key empty", "a/b/", "", "file.txt", "file.txt"},
		{"dest is file, key not empty", "a/b", "dst", "file.txt", "dst"},
		{"dest is file, key empty", "a/b", "", "file.txt", "file.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveDestKey(tt.destPath, tt.destKey, tt.srcBase)
			if got != tt.want {
				t.Errorf("ResolveDestKey(%q, %q, %q) = %q, want %q",
					tt.destPath, tt.destKey, tt.srcBase, got, tt.want)
			}
		})
	}
}

func TestNormalizePrefix(t *testing.T) {
	tests := []struct {
		name string
		path string
		key  string
		want string
	}{
		{"trailing slash appends to key", "bucket/", "key", "key/"},
		{"no trailing slash keeps key", "bucket", "key", "key"},
		{"empty key", "bucket/", "", ""},
		{"double slash normalize", "bucket/", "/", "/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizePrefix(tt.path, tt.key)
			if got != tt.want {
				t.Errorf("NormalizePrefix(%q, %q) = %q, want %q",
					tt.path, tt.key, got, tt.want)
			}
		})
	}
}
