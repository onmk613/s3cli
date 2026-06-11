package httptracer

const (
	DefaultMaxLogLen   = 64 << 10
)

// Options 配置调试传输层的行为
type Options struct {
	MaxLogLen int
	Tag string
	DumpRequestBody bool
}

func (o *Options) applyDefaults() {
	if o.MaxLogLen <= 0 {
		o.MaxLogLen = DefaultMaxLogLen
	}
	if o.Tag == "" {
		o.Tag = "s3cli"
	}
}
