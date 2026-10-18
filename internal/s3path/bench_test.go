package s3path

import "testing"

// BenchmarkParse 量化路径解析开销: ls/find 的每个参数都会走一次,
// 且 --include/--exclude 过滤在热路径上反复调用。
func BenchmarkParse(b *testing.B) {
	const input = "prod:my-bucket/dir/sub/file.txt"
	for i := 0; i < b.N; i++ {
		if _, err := Parse(input); err != nil {
			b.Fatal(err)
		}
	}
}
