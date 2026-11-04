package api

import (
	"errors"
	"io"
	"sync"
	"time"
)

// maxBodyDrain 是 Close 时排空的上限: 剩余字节超过它说明响应异常
// (正常响应 Decode 后只剩结尾空白), 直接关闭丢弃连接, 不值得为它传输。
const maxBodyDrain = 64 * 1024

// errBodyStalled 是停滞保护的哨兵错误, 可用 errors.Is 判别。
var errBodyStalled = errors.New("response body stalled: no data received within timeout (server hang or network black hole)")

// drainCloseBody 排空 body 剩余字节后关闭, 用于 Do 内部不经包装直接关闭的路径
// (错误响应 / region 重定向): 让可重试错误的连接也能复用。
func drainCloseBody(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, maxBodyDrain))
	_ = body.Close()
}

// responseBody 包装真实响应体: Read 带停滞保护, Close 先排空再关闭。
// timeout <= 0 时退化为纯排空语义, 不启动看门狗。
type responseBody struct {
	inner   io.ReadCloser
	timeout time.Duration // <=0 表示禁用停滞保护

	timer   *time.Timer
	stalled chan struct{} // 看门狗超时后关闭
	done    chan struct{} // 读尽/关闭后关闭, 结束看门狗

	closeOnce sync.Once
	stopOnce  sync.Once
	closeErr  error
}

func newResponseBody(inner io.ReadCloser, timeout time.Duration) *responseBody {
	b := &responseBody{
		inner:   inner,
		timeout: timeout,
		stalled: make(chan struct{}),
		done:    make(chan struct{}),
	}
	if timeout > 0 {
		b.timer = time.NewTimer(timeout)
		go b.watchdog()
	}
	return b
}

// watchdog 在超时触发时关闭 stalled 并断开底层连接:
// 关闭 net 连接会解除可能在阻塞中的 Read, 使其带着错误返回。
func (b *responseBody) watchdog() {
	defer b.timer.Stop()
	select {
	case <-b.timer.C:
		close(b.stalled)
		_ = b.inner.Close()
	case <-b.done:
	}
}

func (b *responseBody) stopWatchdog() {
	b.stopOnce.Do(func() { close(b.done) })
}

func (b *responseBody) stalledErr() error {
	select {
	case <-b.stalled:
		return errBodyStalled
	default:
		return nil
	}
}

func (b *responseBody) Read(p []byte) (int, error) {
	n, err := b.inner.Read(p)
	if n > 0 && b.timer != nil {
		b.timer.Reset(b.timeout)
	}
	if err != nil {
		// 读尽或底层出错: 看门狗没有存在意义了。
		b.stopWatchdog()
		if stallErr := b.stalledErr(); stallErr != nil {
			return n, stallErr
		}
		return n, err
	}
	// 读到了数据, 但看门狗恰好已触发 (且已关闭底层连接):
	// 一并按停滞报告, 让调用方拿到可归因的错误。
	if stallErr := b.stalledErr(); stallErr != nil {
		return n, stallErr
	}
	return n, nil
}

func (b *responseBody) Close() error {
	b.closeOnce.Do(func() {
		// 排空仍走本类型的 Read: 停滞保护照常生效, 异常服务端吊住排空读时
		// 看门狗会断开连接, 不会把 Close 卡成永久阻塞。
		_, _ = io.Copy(io.Discard, io.LimitReader(b, maxBodyDrain))
		b.stopWatchdog()
		b.closeErr = b.inner.Close()
	})
	return b.closeErr
}
