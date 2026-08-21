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

func TestHeaderAndUserAgentTransportsCloneAndApplyValues(t *testing.T) {
	capture := &captureTransport{}
	rt, err := newHeaderTransport(newUserAgentTransport(capture, "s3cli-test", "ci"), []string{"X-Test:1", "X-Test=2"})
	if err != nil {
		t.Fatal(err)
	}
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
	if got := capture.req.Header.Values("X-Test"); len(got) != 2 {
		t.Fatalf("headers = %v", got)
	}
	if req.Header.Get("X-Test") != "" || req.Header.Get("User-Agent") != "original" {
		t.Fatal("caller request was mutated")
	}
}

func TestNewHeaderTransport(t *testing.T) {
	t.Run("both separators picks earliest", func(t *testing.T) {
		capture := &captureTransport{}
		rt, err := newHeaderTransport(capture, []string{"X-Both:a=b"})
		if err != nil {
			t.Fatal(err)
		}
		req, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
		if _, err := rt.RoundTrip(req); err != nil {
			t.Fatal(err)
		}
		if got := capture.req.Header.Get("X-Both"); got != "a=b" {
			t.Fatalf("X-Both = %q", got)
		}
	})

	t.Run("empty key rejected", func(t *testing.T) {
		if _, err := newHeaderTransport(&captureTransport{}, []string{":value"}); err == nil {
			t.Fatal("expected error for empty key")
		}
		if _, err := newHeaderTransport(&captureTransport{}, []string{"=value"}); err == nil {
			t.Fatal("expected error for empty key")
		}
	})
}

// TestHeaderTransportSpecialKeys Host / Content-Length 是 SigV4 签名头,
// transport 层的事后改写必然导致 SignatureDoesNotMatch, 必须在构造期拒绝。
func TestHeaderTransportSpecialKeys(t *testing.T) {
	t.Run("host rejected with explanation", func(t *testing.T) {
		_, err := newHeaderTransport(&captureTransport{}, []string{"Host:custom.example.com"})
		if err == nil {
			t.Fatal("expected Host override to be rejected")
		}
		if !strings.Contains(err.Error(), "SigV4") {
			t.Errorf("error should explain the SigV4 reason, got: %v", err)
		}
	})

	t.Run("content-length rejected", func(t *testing.T) {
		if _, err := newHeaderTransport(&captureTransport{}, []string{"Content-Length: 42"}); err == nil {
			t.Fatal("expected Content-Length override to be rejected")
		}
	})

	t.Run("other headers still work", func(t *testing.T) {
		capture := &captureTransport{}
		rt, err := newHeaderTransport(capture, []string{"X-Extra:1"})
		if err != nil {
			t.Fatal(err)
		}
		req, _ := http.NewRequest(http.MethodPut, "https://example.test", nil)
		if _, err := rt.RoundTrip(req); err != nil {
			t.Fatal(err)
		}
		if capture.req.Header.Get("X-Extra") != "1" {
			t.Errorf("X-Extra = %q, want 1", capture.req.Header.Get("X-Extra"))
		}
	})
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
