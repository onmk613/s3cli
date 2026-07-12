package client

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	myprint "s3cli/pkg/fmtutil"
	"sync/atomic"
	"time"
)

var globalSeq atomic.Int64

type Transport struct {
	base http.RoundTripper
	tag  string
}

func NewDumper(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &Transport{
		base: base,
		tag:  "HTTP",
	}
}

var sensitiveHeaders = map[string]struct{}{
	"Authorization": {}, "X-Amz-Security-Token": {}, "Cookie": {}, "Set-Cookie": {},
	"X-Amz-Server-Side-Encryption-Customer-Key": {},
}

func redactedRequest(req *http.Request) *http.Request {
	clone := req.Clone(req.Context())
	for key := range clone.Header {
		if _, sensitive := sensitiveHeaders[http.CanonicalHeaderKey(key)]; sensitive {
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
		if _, sensitive := sensitiveHeaders[http.CanonicalHeaderKey(key)]; sensitive {
			clone.Header.Set(key, "REDACTED")
		}
	}
	return clone
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	seq := globalSeq.Add(1)
	fmt.Println()
	// dump request
	if req != nil {
		if b, err := httputil.DumpRequestOut(redactedRequest(req), false); err != nil {
			myprint.PrintfRed("[%v #%v] dump request error: %v\n", t.tag, seq, err)
		} else {
			myprint.PrintfBlue("========== %v REQUEST #%v ==========\n", t.tag, seq)
			myprint.PrintlnBlue(string(b))
		}
	}

	start := time.Now()
	resp, err := t.base.RoundTrip(req)
	dur := time.Since(start)
	if err != nil {
		myprint.PrintfRed("[%v #%v] error after %v: %v\n", t.tag, seq, dur, err)
		return resp, err
	}

	// Debug logging intentionally excludes response bodies: error bodies may
	// contain tenant data and can be arbitrarily large.
	if resp != nil {
		if b, err := httputil.DumpResponse(redactedResponse(resp), false); err != nil {
			myprint.PrintfRed("[%v #%v] dump response error: %v\n", t.tag, seq, err)
		} else {
			fmt.Println()
			myprint.PrintfBlue("========== %v RESPONSE #%v (%v) ==========\n", t.tag, seq, dur)
			myprint.PrintlnBlue(string(b))
		}
	}

	return resp, nil
}
