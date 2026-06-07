package httptracer

const (
	DefaultMaxBodyDump = 64 << 10
	DefaultMaxLogLen   = 64 << 10
)

// Options 配置调试传输层的行为
type Options struct {

	// MaxBodyDump 响应体最大转储字节数，超过则省略
	MaxBodyDump int64

	// MaxLogLen 单次日志最大长度，超过则截断
	MaxLogLen int

	// Tag 日志前缀标识，默认 "s3cli"
	Tag string

	// DumpRequestBody 是否转储请求体
	DumpRequestBody bool
}

func (o *Options) applyDefaults() {
	if o.MaxBodyDump <= 0 {
		o.MaxBodyDump = DefaultMaxBodyDump
	}
	if o.MaxLogLen <= 0 {
		o.MaxLogLen = DefaultMaxLogLen
	}
	if o.Tag == "" {
		o.Tag = "s3cli"
	}
}
