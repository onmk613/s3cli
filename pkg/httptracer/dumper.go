// Package httptracer 提供 HTTP 请求/响应的抓包 dump 功能，用于 --debug 模式调试。
package httptracer

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	myprint "s3cli/pkg/fmtutil"
	"time"
)

type dumper struct {
	tag         string
	maxLogLen   int
	dumpReqBody bool
}

func newDumper(opts *Options) *dumper {
	return &dumper{
		tag:         opts.Tag,
		maxLogLen:   opts.MaxLogLen,
		dumpReqBody: opts.DumpRequestBody,
	}
}

func (d *dumper) DumpRequest(seq int64, req *http.Request) {
	if req == nil {
		return
	}
	b, err := httputil.DumpRequestOut(req, d.dumpReqBody)
	if err != nil {
		myprint.PrintfRed("[%v #%v] dump request error: %v\n", d.tag, seq, err)
		return
	}
	myprint.PrintfBlue("========== %v REQUEST #%v ==========\n", d.tag, seq)
	myprint.PrintlnBlue(d.sanitize(string(b)))
}

func (d *dumper) DumpResponse(seq int64, resp *http.Response, dur time.Duration) {
	if resp == nil {
		return
	}

	// 只在错误状态码时 dump body，正常响应只展示 header + duration
	dumpBody := resp.StatusCode >= 400

	b, err := httputil.DumpResponse(resp, dumpBody)
	if err != nil {
		myprint.PrintfRed("[%v #%v] dump response error: %v\n", d.tag, seq, err)
		return
	}

	myprint.PrintfBlue("========== %v RESPONSE #%v (%v) ==========\n", d.tag, seq, dur)
	myprint.PrintlnBlue(d.sanitize(string(b)))
}

func (d *dumper) DumpError(seq int64, dur time.Duration, err error) {
	myprint.PrintfRed("[%v #%v] error after %v: %v\n", d.tag, seq, dur, err)
}

func (d *dumper) sanitize(s string) string {
	if len(s) > d.maxLogLen {
		s = s[:d.maxLogLen] + fmt.Sprintf("\n...[truncated %v bytes]", len(s)-d.maxLogLen)
	}
	return s
}
