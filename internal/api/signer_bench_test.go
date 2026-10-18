package api

import (
	"net/http"
	"testing"
	"time"
)

// BenchmarkSignV4 量化纯标准库 SigV4 的签名开销。
// 项目宣称"零 SDK 依赖、轻量"，签名是最热的纯 CPU 路径，必须有可复现的数字。
func BenchmarkSignV4(b *testing.B) {
	req, err := http.NewRequest(http.MethodGet, "https://s3.example.com/bucket/dir/key.txt?list-type=2&prefix=dir%2F", nil)
	if err != nil {
		b.Fatal(err)
	}
	req.Header.Set("X-Amz-Content-Sha256", unsignedPayload)
	req.Header.Set("Content-Type", "text/plain")
	now := time.Unix(1_800_000_000, 0).UTC()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		signV4(req, "AKIAEXAMPLE", "secret-key", "us-east-1", unsignedPayload, now)
	}
}
