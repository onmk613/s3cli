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
