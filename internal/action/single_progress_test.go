package action

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/onmk613/s3cli/pkg/api"
)

// TestMultipartCopyPartSize 分片复制的进度分片: 默认 256MiB,
// 对象极大时按 10000 片上限抬高，且不超过协议单片上限 5GiB。
func TestMultipartCopyPartSize(t *testing.T) {
	const mib = int64(1 << 20)
	if got := multipartCopyPartSize(6 << 30); got != 256*mib {
		t.Errorf("6GiB part size = %d, want 256MiB", got)
	}
	// 10TiB / 10000 = 1.02GiB，超过 256MiB，需要抬高。
	tenTiB := int64(10) << 40
	if got := multipartCopyPartSize(tenTiB); got <= 256*mib || got > maxMultipartPartSize {
		t.Errorf("10TiB part size = %d, want in (256MiB, 5GiB]", got)
	}
	// 超过 10000 * 5GiB 时会被协议上限截断，由分片数校验报错。
	if got := multipartCopyPartSize(100 << 40); got != maxMultipartPartSize {
		t.Errorf("100TiB part size = %d, want 5GiB cap", got)
	}
}

// TestCopyMultipartSameEndpointReportsProgress 断言带进度的分片复制
// 每片完成即上报，且上报总量等于对象大小。
func TestCopyMultipartSameEndpointReportsProgress(t *testing.T) {
	state := &largeCopyServer{}
	server := httptest.NewServer(state.handler())
	defer server.Close()

	client := &Action{S3: actionTestClient(t, server.URL, nil), Alias: "test", Ctx: context.Background()}
	const total = 6 << 30 // 6GiB
	head := &api.HeadObjectOutput{ContentLength: total, ContentType: "application/octet-stream"}

	var reported int64
	err := client.copyMultipartSameEndpointParts("bucket", "src-big", "bucket", "dst-big", head,
		&api.CopyObjectOptions{}, multipartCopyPartSize(total), func(n int64) { reported += n })
	if err != nil {
		t.Fatalf("multipart copy with progress: %v", err)
	}
	if reported != total {
		t.Fatalf("reported = %d, want %d", reported, total)
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	if want := int((total + 256<<20 - 1) / (256 << 20)); len(state.parts) != want {
		t.Fatalf("parts = %d, want %d (256MiB each)", len(state.parts), want)
	}
	if !state.completed {
		t.Error("multipart upload was never completed")
	}
}

// TestIsCopyNotImplemented 只有"端点不支持 UploadPartCopy"类错误才允许回退，
// 权限/网络错误必须原样上报。
func TestIsCopyNotImplemented(t *testing.T) {
	for _, code := range []string{"NotImplemented", "NotSupported", "MethodNotAllowed"} {
		if !isCopyNotImplemented(&api.ErrorResponse{Code: code}) {
			t.Errorf("%s should allow fallback", code)
		}
	}
	if isCopyNotImplemented(&api.ErrorResponse{Code: "AccessDenied", StatusCode: http.StatusForbidden}) {
		t.Error("AccessDenied must not allow fallback")
	}
	if isCopyNotImplemented(nil) {
		t.Error("nil must not allow fallback")
	}
}
