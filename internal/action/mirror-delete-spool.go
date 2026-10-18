// mirror-delete-spool.go 把 mirror --remove 的待删除 key 落到临时文件,
// 避免超大目标端把整份删除清单驻留内存。
//
// 删除阶段必须等所有复制成功后才能开始 (见 copyAndDelete 的围栏语义),
// 因此不能边归并边删; 但也没必要把 key 全量放内存: JSON lines + 临时文件
// 让内存占用降到 O(batch), 同时保留 --max-delete 的"全量计数后再删"语义。

package action

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/onmk613/s3cli/internal/i18n"
)

type deleteSpool struct {
	file  *os.File
	w     *bufio.Writer
	enc   *json.Encoder
	count int64
}

func newDeleteSpool() (*deleteSpool, error) {
	f, err := os.CreateTemp("", "s3cli-mirror-delete-*")
	if err != nil {
		return nil, fmt.Errorf(i18n.T("create delete spool: %w", "创建删除清单临时文件：%w"), err)
	}
	w := bufio.NewWriterSize(f, 64*1024)
	return &deleteSpool{file: f, w: w, enc: json.NewEncoder(w)}, nil
}

func (s *deleteSpool) add(key string) error {
	if err := s.enc.Encode(key); err != nil {
		return fmt.Errorf(i18n.T("write delete spool: %w", "写入删除清单：%w"), err)
	}
	s.count++
	return nil
}

// close 刷新并删除临时文件; 可重复调用 (nil 安全)。
func (s *deleteSpool) close() {
	if s == nil {
		return
	}
	if s.w != nil {
		_ = s.w.Flush()
		s.w = nil
	}
	if s.file != nil {
		_ = s.file.Close()
		_ = os.Remove(s.file.Name())
		s.file = nil
	}
}

// forEachBatch 回卷临时文件并按 batchSize 逐批回调, 内存占用 O(batchSize)。
func (s *deleteSpool) forEachBatch(ctx context.Context, batchSize int, fn func([]string) error) error {
	if batchSize <= 0 {
		batchSize = s3DeleteBatchSize
	}
	if err := s.w.Flush(); err != nil {
		return fmt.Errorf(i18n.T("flush delete spool: %w", "刷新删除清单：%w"), err)
	}
	if _, err := s.file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf(i18n.T("rewind delete spool: %w", "回卷删除清单：%w"), err)
	}

	dec := json.NewDecoder(bufio.NewReaderSize(s.file, 64*1024))
	batch := make([]string, 0, batchSize)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var key string
		err := dec.Decode(&key)
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf(i18n.T("read delete spool: %w", "读取删除清单：%w"), err)
		}
		batch = append(batch, key)
		if len(batch) >= batchSize {
			if err := fn(batch); err != nil {
				return err
			}
			batch = batch[:0]
		}
	}
	if len(batch) > 0 {
		if err := fn(batch); err != nil {
			return err
		}
	}
	return nil
}
