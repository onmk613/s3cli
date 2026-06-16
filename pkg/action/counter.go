package action

import (
	"io"
	"sync/atomic"
)

// ReportFunc 上报本次新增的字节数
// 在 StreamConfig 中实现为 progress 中的 AddTotalSizeDone 方法
// 实现实时更新进度
type ReportFunc func(n int64)

// readerAtCounter 包装一个 io.ReadSeeker
type readerAtCounter struct {
	src    readSeekerAt
	maxOff atomic.Int64 // 已观测到的最高读偏移
	report ReportFunc
}

// readSeekerAt 是 manager 上传单个 *os.File 时实际使用的接口集合
type readSeekerAt interface {
	io.Reader
	io.ReaderAt
	io.Seeker
}

// NewUploadCounter 包装上传源，按读取进度实时上报字节增量
func NewUploadCounter(src readSeekerAt, report ReportFunc) io.Reader {
	return &readerAtCounter{src: src, report: report}
}

func (c *readerAtCounter) advance(newOff int64) {
	for {
		cur := c.maxOff.Load()
		if newOff <= cur {
			return
		}
		if c.maxOff.CompareAndSwap(cur, newOff) {
			if c.report != nil {
				c.report(newOff - cur)
			}
			return
		}
	}
}

func (c *readerAtCounter) Read(p []byte) (int, error) {
	// 顺序读：用 Seek(0, Current) 也可，但 manager 对 *os.File 主要走 ReadAt。
	n, err := c.src.Read(p)
	if n > 0 {
		if off, serr := c.src.Seek(0, io.SeekCurrent); serr == nil {
			c.advance(off)
		}
	}
	return n, err
}

func (c *readerAtCounter) ReadAt(p []byte, off int64) (int, error) {
	n, err := c.src.ReadAt(p, off)
	if n > 0 {
		c.advance(off + int64(n))
	}
	return n, err
}

func (c *readerAtCounter) Seek(offset int64, whence int) (int64, error) {
	return c.src.Seek(offset, whence)
}

// writerAtCounter 包装一个 io.WriterAt
type writerAtCounter struct {
	dst    io.WriterAt
	maxOff atomic.Int64
	report ReportFunc
}

// NewDownloadCounter 包装下载目标，按写入进度实时上报字节增量
func NewDownloadCounter(dst io.WriterAt, report ReportFunc) io.WriterAt {
	return &writerAtCounter{dst: dst, report: report}
}

func (c *writerAtCounter) WriteAt(p []byte, off int64) (int, error) {
	n, err := c.dst.WriteAt(p, off)
	if n > 0 {
		newOff := off + int64(n)
		for {
			cur := c.maxOff.Load()
			if newOff <= cur {
				break
			}
			if c.maxOff.CompareAndSwap(cur, newOff) {
				if c.report != nil {
					c.report(newOff - cur)
				}
				break
			}
		}
	}
	return n, err
}

// readerCounter 包装一个普通 io.Reader，在每次顺序读取时按读到的字节数上报
type readerCounter struct {
	src    io.Reader
	report ReportFunc
}

// NewReaderCounter 包装顺序读取的 Reader，按读取字节数实时上报增量
func NewReaderCounter(src io.Reader, report ReportFunc) io.Reader {
	return &readerCounter{src: src, report: report}
}

func (c *readerCounter) Read(p []byte) (int, error) {
	n, err := c.src.Read(p)
	if n > 0 && c.report != nil {
		c.report(int64(n))
	}
	return n, err
}
