package action

import (
	"io"
	"strings"
	"sync"
	"testing"
)

// nopWriterAt 丢弃写入，仅返回写入长度，用于测试 writerAtCounter。
type nopWriterAt struct{}

func (nopWriterAt) WriteAt(p []byte, off int64) (int, error) { return len(p), nil }

func TestUploadCounter_MonotonicOnRetry(t *testing.T) {
	var got int64
	src := strings.NewReader(strings.Repeat("x", 300))
	r, ok := NewUploadCounter(src, func(n int64) { got += n }).(*readerAtCounter)
	if !ok {
		t.Fatalf("NewUploadCounter 应返回 *readerAtCounter")
	}

	buf := make([]byte, 100)
	r.ReadAt(buf, 0)   // 0 -> 100
	r.ReadAt(buf, 100) // 100 -> 200
	r.ReadAt(buf, 0)   // 重试低偏移，不应增长

	if got != 200 {
		t.Fatalf("上传计数应为 200（按最高偏移），实际 %d", got)
	}
}

func TestUploadCounter_OutOfOrder(t *testing.T) {
	var got int64
	src := strings.NewReader(strings.Repeat("x", 300))
	r := NewUploadCounter(src, func(n int64) { got += n }).(*readerAtCounter)

	buf := make([]byte, 100)
	r.ReadAt(buf, 200) // 直接跳到尾段 200 -> 300
	r.ReadAt(buf, 0)   // 乱序回到开头，最高偏移已是 300，不增长
	r.ReadAt(buf, 100)

	if got != 300 {
		t.Fatalf("乱序读取应按最高偏移上报 300，实际 %d", got)
	}
}

func TestUploadCounter_SequentialRead(t *testing.T) {
	var got int64
	src := strings.NewReader(strings.Repeat("x", 250))
	r := NewUploadCounter(src, func(n int64) { got += n })

	// 顺序 Read 直到 EOF
	buf := make([]byte, 64)
	for {
		_, err := r.Read(buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Read 出错: %v", err)
		}
	}
	if got != 250 {
		t.Fatalf("顺序读取应上报 250，实际 %d", got)
	}
}

func TestUploadCounter_NilReport(t *testing.T) {
	// report 为 nil 时不应 panic。
	r := NewUploadCounter(strings.NewReader("abc"), nil)
	if _, err := io.ReadAll(r); err != nil {
		t.Fatalf("nil report 不应出错: %v", err)
	}
}

func TestDownloadCounter_MonotonicOnRetry(t *testing.T) {
	var got int64
	w := NewDownloadCounter(nopWriterAt{}, func(n int64) { got += n })

	w.WriteAt(make([]byte, 100), 0)   // 0 -> 100
	w.WriteAt(make([]byte, 100), 100) // 100 -> 200
	w.WriteAt(make([]byte, 100), 0)   // 重试，不增长

	if got != 200 {
		t.Fatalf("下载计数应为 200，实际 %d", got)
	}
}

func TestDownloadCounter_Concurrent(t *testing.T) {
	var got int64
	var mu sync.Mutex
	w := NewDownloadCounter(nopWriterAt{}, func(n int64) {
		mu.Lock()
		got += n
		mu.Unlock()
	})

	const parts = 50
	const partSize = 64
	var wg sync.WaitGroup
	for i := 0; i < parts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w.WriteAt(make([]byte, partSize), int64(i*partSize))
		}(i)
	}
	wg.Wait()

	if want := int64(parts * partSize); got != want {
		t.Fatalf("并发写入应上报 %d，实际 %d", want, got)
	}
}

func TestReaderCounter_Accumulates(t *testing.T) {
	var got int64
	const data = "hello world!"
	r := NewReaderCounter(strings.NewReader(data), func(n int64) { got += n })

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll 出错: %v", err)
	}
	if string(out) != data {
		t.Fatalf("内容不应被改变，实际 %q", out)
	}
	if got != int64(len(data)) {
		t.Fatalf("ReaderCounter 应上报 %d，实际 %d", len(data), got)
	}
}

func TestReaderCounter_NilReport(t *testing.T) {
	r := NewReaderCounter(strings.NewReader("abc"), nil)
	if _, err := io.ReadAll(r); err != nil {
		t.Fatalf("nil report 不应出错: %v", err)
	}
}
