package client

import "net/http"

// userAgentTransport 在请求发出前改写 User-Agent 头
type userAgentTransport struct {
	base     http.RoundTripper
	override string
	suffix   string
}

func newUserAgentTransport(base http.RoundTripper, override, suffix string) http.RoundTripper {
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
	// 克隆请求以避免修改调用方持有的 *http.Request
	clone := req.Clone(req.Context())
	clone.Header.Set("User-Agent", ua)
	return t.base.RoundTrip(clone)
}
