package action

import (
	"context"
	"strings"
	"testing"
)

// TestDeleteSpoolRoundTrip 验证删除清单能原样读回含换行/引号的 key,
// 并按批次回调 (JSON lines 而不是按行裸文本, 避免 key 中的换行破坏格式)。
func TestDeleteSpoolRoundTrip(t *testing.T) {
	spool, err := newDeleteSpool()
	if err != nil {
		t.Fatal(err)
	}
	defer spool.close()

	keys := []string{"plain.txt", "with\nnewline.txt", `with"quote.txt`}
	for _, k := range keys {
		if err := spool.add(k); err != nil {
			t.Fatal(err)
		}
	}

	var got []string
	if err := spool.forEachBatch(context.Background(), 2, func(batch []string) error {
		got = append(got, batch...)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, "|") != strings.Join(keys, "|") {
		t.Fatalf("round-trip = %q, want %q", got, keys)
	}
	if spool.count != int64(len(keys)) {
		t.Fatalf("count = %d, want %d", spool.count, len(keys))
	}
}
