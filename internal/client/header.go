// header.go 解析 CLI 的 -H/--header 自定义请求头。
//
// 这些头由 api 层在 SigV4 签名之前注入 (api.Options.ExtraHeaders), 而不是通过
// RoundTripper 在签名之后追加 —— 签名覆盖除 Authorization / User-Agent /
// Accept-Encoding 之外的全部头, 事后注入会让服务端算出不同的规范请求, 结果是
// 毫无线索的 SignatureDoesNotMatch。

package client

import (
	"fmt"
	"net/http"
	"strings"
)

// parseCustomHeaders 把 "key:value" / "key=value" 形式的重复参数解析为 http.Header。
//
// 分隔符取 ':' 与 '=' 中先出现的一个 (兼容两种写法)。返回 nil 表示未指定任何头。
func parseCustomHeaders(items []string) (http.Header, error) {
	if len(items) == 0 {
		return nil, nil
	}
	headers := make(http.Header, len(items))
	for _, raw := range items {
		ci := strings.IndexByte(raw, ':')
		ei := strings.IndexByte(raw, '=')

		var sep int
		switch {
		case ci >= 0 && ei >= 0:
			sep = min(ci, ei)
		case ci >= 0:
			sep = ci
		case ei >= 0:
			sep = ei
		default:
			return nil, fmt.Errorf("invalid header %q, expected format key:value or key=value", raw)
		}

		key := strings.TrimSpace(raw[:sep])
		val := strings.TrimSpace(raw[sep+1:])
		if key == "" {
			return nil, fmt.Errorf("invalid header %q, key is empty", raw)
		}
		headers.Add(key, val)
	}
	return headers, nil
}
