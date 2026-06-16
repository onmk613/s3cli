package client

import (
	"fmt"
	"net/http"
	"strings"
)

// customHeaderTransport 在请求发出前注入自定义 HTTP header。
type customHeaderTransport struct {
	base    http.RoundTripper
	headers http.Header
}

// parseHeaders 把 ["Key: Value", "Key2=Value2"] 解析为 http.Header。
// 支持 ":" 或 "=" 作为分隔符(取最先出现的那个); key 不能为空。
// 同名 key 多次出现会追加为多值 header。
func parseHeaders(items []string) (http.Header, error) {
	if len(items) == 0 {
		return nil, nil
	}
	h := http.Header{}
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
		h.Add(key, val)
	}
	return h, nil
}

// newCustomHeaderTransport 当 headers 为空时返回 base 本身(零开销)。
func newCustomHeaderTransport(base http.RoundTripper, headers http.Header) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	if len(headers) == 0 {
		return base
	}
	return &customHeaderTransport{base: base, headers: headers}
}

func (t *customHeaderTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// 克隆请求, 避免修改调用方持有的 *http.Request(SDK 可能重试)。
	clone := req.Clone(req.Context())
	for k, vs := range t.headers {
		clone.Header.Del(k) // 用户指定的值覆盖 SDK 默认值
		for _, v := range vs {
			clone.Header.Add(k, v)
		}
	}
	return t.base.RoundTrip(clone)
}
