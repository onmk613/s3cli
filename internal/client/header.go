package client

import (
	"fmt"
	"net/http"
	"strings"
)

// headerTransport 在请求发出前注入自定义 HTTP header。
type headerTransport struct {
	base    http.RoundTripper
	headers http.Header
}

func newHeaderTransport(base http.RoundTripper, items []string) (http.RoundTripper, error) {
	headers := http.Header{}
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
			return base, fmt.Errorf("invalid header %q, expected format key:value or key=value", raw)
		}

		key := strings.TrimSpace(raw[:sep])
		val := strings.TrimSpace(raw[sep+1:])
		if key == "" {
			return base, fmt.Errorf("invalid header %q, key is empty", raw)
		}
		headers.Add(key, val)
	}
	return &headerTransport{base: base, headers: headers}, nil
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
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
