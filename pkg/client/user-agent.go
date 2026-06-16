package client

import "net/http"

// userAgentTransport 在请求发出前改写 User-Agent 头。
//   - override 非空: 直接覆盖整个 User-Agent。
//   - suffix 非空: 追加到原 User-Agent 末尾(以空格分隔)。
//
// override 优先于 suffix; 若 override 已设置, suffix 仍会追加到覆盖值之后。
type userAgentTransport struct {
	base     http.RoundTripper
	override string
	suffix   string
}

// newUserAgentTransport 当 override 与 suffix 均为空时返回 base 本身(零开销)。
func newUserAgentTransport(base http.RoundTripper, override, suffix string) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	if override == "" && suffix == "" {
		return base
	}
	return &userAgentTransport{base: base, override: override, suffix: suffix}
}

func (t *userAgentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ua := req.Header.Get("User-Agent")
	if t.override != "" {
		ua = t.override
	}
	if t.suffix != "" {
		if ua == "" {
			ua = t.suffix
		} else {
			ua = ua + " " + t.suffix
		}
	}
	// 克隆请求以避免修改调用方持有的 *http.Request(SDK 可能重试)。
	clone := req.Clone(req.Context())
	clone.Header.Set("User-Agent", ua)
	return t.base.RoundTrip(clone)
}
