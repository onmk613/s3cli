package client

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// headerTransport 在请求发出前注入自定义 HTTP header。
// Host 与 Content-Length 是 http.Request 的结构字段, 写在 Header map 里会被
// http.Client 忽略 (静默无效); 这里单独处理, 保证用户意图真正生效。
type headerTransport struct {
	base          http.RoundTripper
	headers       http.Header
	host          string
	contentLength int64
	hasContentLen bool
}

func newHeaderTransport(base http.RoundTripper, items []string) (http.RoundTripper, error) {
	t := &headerTransport{base: base, headers: http.Header{}}
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
		switch http.CanonicalHeaderKey(key) {
		case "Host":
			t.host = val
		case "Content-Length":
			n, err := strconv.ParseInt(val, 10, 64)
			if err != nil || n < 0 {
				return base, fmt.Errorf("invalid header %q: Content-Length must be a non-negative integer", raw)
			}
			t.contentLength = n
			t.hasContentLen = true
		default:
			t.headers.Add(key, val)
		}
	}
	return t, nil
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
	if t.host != "" {
		clone.Host = t.host
	}
	if t.hasContentLen {
		clone.ContentLength = t.contentLength
	}
	return t.base.RoundTrip(clone)
}
