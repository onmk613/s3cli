package httptracer

import (
	"net/http"
	"sync/atomic"
	"time"
)

var globalSeq atomic.Int64

type Transport struct {
	base   http.RoundTripper
	dumper *dumper
}

func New(base http.RoundTripper, opts *Options) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	if opts == nil {
		opts = &Options{}
	}
	opts.applyDefaults()

	return &Transport{
		base:   base,
		dumper: newDumper(opts),
	}
}

// RoundTrip 实现 http.RoundTripper 接口
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	seq := globalSeq.Add(1)
	start := time.Now()

	t.dumper.DumpRequest(seq, req)

	resp, err := t.base.RoundTrip(req)
	dur := time.Since(start)

	if err != nil {
		t.dumper.DumpError(seq, dur, err)
		return resp, err
	}

	t.dumper.DumpResponse(seq, resp, dur)
	return resp, nil
}
