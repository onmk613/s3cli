package config

import (
	"net/url"
	"strings"
)

func validateCustomTemplate(tpl, placeholder string) bool {
	// 1) 必须包含占位符
	if !strings.Contains(tpl, placeholder) {
		return false
	}

	// 2) 占位符不能在最末尾
	if strings.HasSuffix(tpl, placeholder) {
		return false
	}

	// 3) 占位符只能出现一次
	if strings.Count(tpl, placeholder) > 1 {
		return false
	}

	// 4) 用一个假 bucket 名替换占位符，验证结果是否为合法 URL
	testURL := strings.ReplaceAll(tpl, BucketPlaceholder, "test-bucket")
	u, err := url.Parse(testURL)
	if err != nil {
		return false
	}

	// 5) host 不能为空
	if u.Host == "" {
		return false
	}

	// 6) host 中不能有连续的点（如 ..example.com）
	if strings.Contains(u.Host, "..") {
		return false
	}

	return true
}
