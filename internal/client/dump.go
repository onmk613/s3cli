package client

import (
	"net/http"
	"net/http/httputil"
	"strings"
	"sync/atomic"
	"time"

	myprint "github.com/onmk613/s3cli/internal/fmtutil"
)

var globalSeq atomic.Int64

type dumpTransport struct {
	base http.RoundTripper
	tag  string
}

func newDumper(base http.RoundTripper) http.RoundTripper {
	return &dumpTransport{
		base: base,
		tag:  "HTTP",
	}
}

var sensitiveHeaders = map[string]struct{}{
	"Authorization": {}, "Proxy-Authorization": {}, "X-Amz-Security-Token": {}, "Cookie": {}, "Set-Cookie": {},
	"X-Amz-Server-Side-Encryption-Customer-Key": {},
}

// isSensitiveHeader 判断 header 是否应在 --debug 输出中脱敏。
//
// 固定名单只能覆盖已知头; 用户通过 -H 传入的 X-Api-Key / X-Auth-Token
// 等自定义凭据头同样敏感。这里按"名称含敏感词"兜底, 宁可多脱敏一个
// 普通头, 也不要把用户的凭据打进终端/CI 日志。
func isSensitiveHeader(name string) bool {
	if _, ok := sensitiveHeaders[http.CanonicalHeaderKey(name)]; ok {
		return true
	}
	n := strings.ToLower(name)
	for _, needle := range []string{
		"authorization", "credential", "token", "secret", "password",
		"api-key", "apikey", "access-key", "private-key",
	} {
		if strings.Contains(n, needle) {
			return true
		}
	}
	// 形如 X-Custom-Key 的凭据头 (但排除 x-amz-meta-* 这类普通元数据)
	return strings.HasSuffix(n, "-key") && !strings.HasPrefix(n, "x-amz-meta-")
}

// dumpResponse 是 httputil.DumpResponse 的间接层, 便于测试覆盖其错误分支.
var dumpResponse = httputil.DumpResponse

func redactedRequest(req *http.Request) *http.Request {
	clone := req.Clone(req.Context())
	for key := range clone.Header {
		if isSensitiveHeader(key) {
			clone.Header.Set(key, "REDACTED")
		}
	}
	return clone
}

func redactedResponse(resp *http.Response) *http.Response {
	clone := new(http.Response)
	*clone = *resp
	clone.Header = resp.Header.Clone()
	for key := range clone.Header {
		if isSensitiveHeader(key) {
			clone.Header.Set(key, "REDACTED")
		}
	}
	return clone
}

func (t *dumpTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	seq := globalSeq.Add(1)
	myprint.ErrPrintln()
	// dump request
	if req != nil {
		if b, err := httputil.DumpRequestOut(redactedRequest(req), false); err != nil {
			myprint.ErrPrintfRed("[%v #%v] dump request error: %v\n", t.tag, seq, err)
		} else {
			myprint.ErrPrintfBlue("========== %v REQUEST #%v ==========\n", t.tag, seq)
			myprint.ErrPrintlnBlue(string(b))
		}
	}

	start := time.Now()
	resp, err := t.base.RoundTrip(req)
	dur := time.Since(start)
	if err != nil {
		myprint.ErrPrintfRed("[%v #%v] error after %v: %v\n", t.tag, seq, dur, err)
		return resp, err
	}

	// Debug logging intentionally excludes response bodies: error bodies may
	// contain tenant data and can be arbitrarily large.
	if resp != nil {
		if b, err := dumpResponse(redactedResponse(resp), false); err != nil {
			myprint.ErrPrintfRed("[%v #%v] dump response error: %v\n", t.tag, seq, err)
		} else {
			myprint.ErrPrintln()
			myprint.ErrPrintfBlue("========== %v RESPONSE #%v (%v) ==========\n", t.tag, seq, dur)
			myprint.ErrPrintlnBlue(string(b))
		}
	}

	return resp, nil
}
