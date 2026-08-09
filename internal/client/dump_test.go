package client

import (
	"errors"
	"net/http"
	"testing"
)

// roundTripFunc 让测试能以函数形式注入 RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestNewDumper(t *testing.T) {
	if d := newDumper(http.DefaultTransport); d == nil {
		t.Fatal("nil")
	}
	if d := newDumper(nil); d == nil {
		t.Fatal("nil base should fall back")
	}
}

func TestDumpTransportRoundTrip(t *testing.T) {
	t.Run("success dumps request and response", func(t *testing.T) {
		capture := &captureTransport{}
		d := newDumper(capture)
		req, err := http.NewRequest(http.MethodGet, "https://example.test/obj", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := d.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		_ = resp.Body.Close()
		if capture.req == nil {
			t.Fatal("base transport was not invoked")
		}
	})

	t.Run("base error is propagated", func(t *testing.T) {
		boom := errors.New("boom")
		d := newDumper(roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, boom
		}))
		req, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
		if _, err := d.RoundTrip(req); err != boom {
			t.Fatalf("err = %v, want boom", err)
		}
	})

	t.Run("request dump error is handled", func(t *testing.T) {
		// DumpRequestOut 对未知 scheme 报错, 但 base transport 仍被调用.
		called := false
		d := newDumper(roundTripFunc(func(*http.Request) (*http.Response, error) {
			called = true
			return nil, errors.New("unreachable base")
		}))
		req, _ := http.NewRequest(http.MethodGet, "ftp://example.test/x", nil)
		if _, err := d.RoundTrip(req); err == nil {
			t.Fatal("expected error")
		}
		if !called {
			t.Fatal("base transport not invoked after dump error")
		}
	})

	t.Run("nil request", func(t *testing.T) {
		d := newDumper(roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("no req")
		}))
		if _, err := d.RoundTrip(nil); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("response dump error is handled", func(t *testing.T) {
		orig := dumpResponse
		dumpResponse = func(*http.Response, bool) ([]byte, error) {
			return nil, errors.New("dump boom")
		}
		defer func() { dumpResponse = orig }()

		d := newDumper(&captureTransport{})
		req, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
		resp, err := d.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		// dump 失败只记录日志, 不影响请求结果
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		_ = resp.Body.Close()
	})
}

func TestRedactedResponse(t *testing.T) {
	orig := &http.Response{Header: http.Header{}}
	orig.Header.Set("Set-Cookie", "secret=abc")
	orig.Header.Set("X-Amz-Security-Token", "token")
	orig.Header.Set("Content-Type", "text/plain")

	clone := redactedResponse(orig)
	if clone.Header.Get("Set-Cookie") != "REDACTED" {
		t.Error("Set-Cookie should be redacted")
	}
	if clone.Header.Get("X-Amz-Security-Token") != "REDACTED" {
		t.Error("token should be redacted")
	}
	if clone.Header.Get("Content-Type") != "text/plain" {
		t.Error("non-sensitive header should be untouched")
	}
	if orig.Header.Get("Set-Cookie") != "secret=abc" {
		t.Error("original mutated")
	}
}
