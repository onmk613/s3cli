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
	rt := newCustomHeaderTransport(newUserAgentTransport(capture, "s3cli-test", "ci"), http.Header{"X-Test": {"one", "two"}})
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

func TestParseHeaders(t *testing.T) {
	h, err := parseHeaders([]string{"X-A: one", "X-A=two"})
	if err != nil {
		t.Fatal(err)
	}
	if got := h.Values("X-A"); len(got) != 2 {
		t.Fatalf("values = %v", got)
	}
	if _, err := parseHeaders([]string{"missing-separator"}); err == nil {
		t.Fatal("expected validation error")
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
