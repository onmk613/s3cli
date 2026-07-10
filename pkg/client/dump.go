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
	base        http.RoundTripper
	tag         string
	maxLogLen   int
	dumpReqBody bool
}

func NewDumper(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &Transport{
		base:        base,
		tag:         "HTTP",
		maxLogLen:   1024 * 1024, // 1MB
		dumpReqBody: true,
	}
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	seq := globalSeq.Add(1)
	fmt.Println()
	// dump request
	if req != nil {
		if b, err := httputil.DumpRequestOut(req, t.dumpReqBody); err != nil {
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

	// dump response（只在错误状态码时 dump body）
	if resp != nil {
		dumpBody := resp.StatusCode >= 400
		if b, err := httputil.DumpResponse(resp, dumpBody); err != nil {
			myprint.PrintfRed("[%v #%v] dump response error: %v\n", t.tag, seq, err)
		} else {
			fmt.Println()
			myprint.PrintfBlue("========== %v RESPONSE #%v (%v) ==========\n", t.tag, seq, dur)
			myprint.PrintlnBlue(string(b))
		}
	}

	return resp, nil
}
