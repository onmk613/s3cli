package client

import (
	"net/http"
	"testing"
)

// 无 override 且原始请求没有 User-Agent 时, suffix 应直接作为 User-Agent。
func TestUserAgentSuffixOnly(t *testing.T) {
	capture := &captureTransport{}
	rt := newUserAgentTransport(capture, "", "ci")
	req, err := http.NewRequest(http.MethodGet, "https://example.test", nil)
	if err != nil {
		t.Fatal(err)
	}
	// 不设置 User-Agent
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if got := capture.req.Header.Get("User-Agent"); got != "ci" {
		t.Fatalf("user-agent = %q, want %q", got, "ci")
	}
	if req.Header.Get("User-Agent") != "" {
		t.Fatal("caller request was mutated")
	}
}
