package action

import (
	"strings"
	"testing"
)

func TestUrlSignV2(t *testing.T) {
	url := urlSignV2("AKID", "secret", "mybucket", "http://s3.example.com", "path/to/file.txt", 3600)

	// 验证 URL 包含必要字段
	checks := []string{
		"http://s3.example.com/mybucket/",
		"path/to/file.txt",
		"AWSAccessKeyId=AKID",
		"Expires=",
		"Signature=",
	}
	for _, c := range checks {
		if !strings.Contains(url, c) {
			t.Errorf("URL missing %q:\n%s", c, url)
		}
	}
}

func TestUrlSignV2_EmptyPath(t *testing.T) {
	url := urlSignV2("AKID", "secret", "mybucket", "http://s3.example.com", "", 3600)
	if !strings.Contains(url, "http://s3.example.com/mybucket/") {
		t.Errorf("unexpected URL for empty path: %s", url)
	}
}
