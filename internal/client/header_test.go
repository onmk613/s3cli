package client

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type captureTransport struct{ req *http.Request }

func (c *captureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	c.req = req
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok")), Request: req}, nil
}

// TestParseCustomHeaders 覆盖 -H/--header 的解析: 两种分隔符、重复键、
// 以及错误输入。注入时机 (签名前) 由 api 层的测试保证, 这里只测解析。
func TestParseCustomHeaders(t *testing.T) {
	t.Run("colon and equals both accepted, repeated keys accumulate", func(t *testing.T) {
		h, err := parseCustomHeaders([]string{"X-Test:1", "X-Test=2"})
		if err != nil {
			t.Fatal(err)
		}
		if got := h.Values("X-Test"); len(got) != 2 || got[0] != "1" || got[1] != "2" {
			t.Fatalf("X-Test = %v", got)
		}
	})

	t.Run("both separators picks earliest", func(t *testing.T) {
		h, err := parseCustomHeaders([]string{"X-Both:a=b"})
		if err != nil {
			t.Fatal(err)
		}
		if got := h.Get("X-Both"); got != "a=b" {
			t.Fatalf("X-Both = %q", got)
		}
	})

	t.Run("empty key rejected", func(t *testing.T) {
		for _, raw := range []string{":value", "=value", "novalue"} {
			if _, err := parseCustomHeaders([]string{raw}); err == nil {
				t.Fatalf("expected error for %q", raw)
			}
		}
	})

	t.Run("no headers yields nil", func(t *testing.T) {
		h, err := parseCustomHeaders(nil)
		if err != nil || h != nil {
			t.Fatalf("got %v, %v", h, err)
		}
	})
}

// TestUserAgentTransportClonesAndApplies 覆盖 UA 改写 RoundTripper:
// 改写副本, 不污染调用方请求。
func TestUserAgentTransportClonesAndApplies(t *testing.T) {
	capture := &captureTransport{}
	rt := newUserAgentTransport(capture, "s3cli-test", "ci")
	req, err := http.NewRequest(http.MethodGet, "https://example.test", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("User-Agent", "original")
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if capture.req.Header.Get("User-Agent") != "s3cli-test ci" {
		t.Fatalf("user-agent = %q", capture.req.Header.Get("User-Agent"))
	}
	if req.Header.Get("User-Agent") != "original" {
		t.Fatal("caller request was mutated")
	}
}

func TestRedaction(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	req.Header.Set("Authorization", "secret")
	if got := redactedRequest(req).Header.Get("Authorization"); got != "REDACTED" {
		t.Fatalf("authorization = %q", got)
	}
	if req.Header.Get("Authorization") != "secret" {
		t.Fatal("original request was mutated")
	}
}
