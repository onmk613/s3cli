package i18n

import "testing"

// TestLocalizeErrorText 锁定已知内部错误前缀的中文翻译, 并确认:
//   - 嵌套前缀会逐层翻译;
//   - 未知前缀与 API 原始英文保持不变;
//   - 英文界面完全不做翻译。
func TestLocalizeErrorText(t *testing.T) {
	mu.Lock()
	old := lang
	mu.Unlock()
	defer func() {
		mu.Lock()
		lang = old
		mu.Unlock()
	}()

	Resolve("zh")
	cases := []struct{ in, want string }{
		{"check s3 path: 路径 'k' 不存在", "检查 S3 路径：路径 'k' 不存在"},
		{"check s3 path: head object: NoSuchKey: The specified key does not exist.", "检查 S3 路径：读取对象元信息：NoSuchKey: The specified key does not exist."},
		{"download: NoSuchKey: The specified key does not exist.", "下载：NoSuchKey: The specified key does not exist."},
		{"list objects: AccessDenied: Access Denied.", "列出对象：AccessDenied: Access Denied."},
		{"set lifecycle rule b: InternalError", "设置存储桶 b 的生命周期规则：InternalError"},
		{"read multipart part 3: EOF", "读取分段分片 3：EOF"},
		{"save config: permission denied", "保存配置：permission denied"},
		{"empty s3 path", "S3 路径为空"},
		{"some unknown prefix: x", "some unknown prefix: x"},
		{"NoSuchKey: The specified key does not exist.", "NoSuchKey: The specified key does not exist."},
	}
	for _, tc := range cases {
		if got := LocalizeErrorText(tc.in); got != tc.want {
			t.Errorf("LocalizeErrorText(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	Resolve("en")
	if got := LocalizeErrorText("check s3 path: x"); got != "check s3 path: x" {
		t.Errorf("English mode should not translate, got %q", got)
	}
}
