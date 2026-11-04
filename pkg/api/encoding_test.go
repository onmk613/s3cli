package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// storedGzipObject 是"对象本身就是一个 gzip 文件"的字节序列 —— 即
// `aws s3 cp backup.tar.gz s3://...` 或控制台上传 .gz 后桶里真实存储的内容。
// S3 会为它打上 Content-Encoding: gzip, 但那只是元数据, body 必须原样返回。
func storedGzipObject(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte("HELLO-WORLD-THIS-IS-THE-ORIGINAL-STORED-BYTES-1234567890")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// gzipServingHandler 无条件按"存储态"返回: 带 Content-Encoding: gzip 的原始字节。
func gzipServingHandler(t *testing.T, stored []byte, sawAcceptEncoding *string) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sawAcceptEncoding != nil {
			*sawAcceptEncoding = r.Header.Get("Accept-Encoding")
		}
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Length", itoa(len(stored)))
		w.Header().Set("ETag", `"e"`)
		_, _ = w.Write(stored)
	})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// TestGetObjectDoesNotTransparentlyGunzip 是回归测试。
//
// 修复前: newRequest 不设置 Accept-Encoding, 于是 Go 的 http.Transport 自行
// 加上 "Accept-Encoding: gzip", 并在收到 Content-Encoding: gzip 的响应时
// **透明解压**、同时删掉 Content-Encoding 与 Content-Length。结果是 `get`
// 写出的字节与桶里存储的字节不一致 (文本 .gz 会静默变成明文), `get --checksum`
// 必然失败, 跨端 mirror 会改变对象内容。更隐蔽的是 Go 在带 Range 的请求上
// 不做自动解压, 于是同一对象"大文件走 Range 保真、小文件被解压", 结果随大小而变。
//
// 注意 testClient 不注入 Transport, 走的是 http.DefaultTransport
// (DisableCompression=false), 因此本用例专门覆盖"transport 未加固"的路径 ——
// 修复必须落在请求上才能与 transport 无关。
func TestGetObjectDoesNotTransparentlyGunzip(t *testing.T) {
	stored := storedGzipObject(t)
	var sawAE string
	c := testClient(t, gzipServingHandler(t, stored, &sawAE))

	out, err := c.GetObject(context.Background(), "bucket", "obj.gz", &GetObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = out.Body.Close() }()

	got, err := io.ReadAll(out.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, stored) {
		t.Fatalf("GetObject returned %d bytes, want the %d stored bytes verbatim\n got: %q",
			len(got), len(stored), got)
	}
	if out.ContentEncoding != "gzip" {
		t.Fatalf("ContentEncoding = %q, want gzip (transport must not strip it)", out.ContentEncoding)
	}
	if out.ContentLength != int64(len(stored)) {
		t.Fatalf("ContentLength = %d, want %d", out.ContentLength, len(stored))
	}

	// 请求必须显式声明不压缩, 否则由 transport 决定, 行为不可移植。
	if sawAE != "identity" {
		t.Fatalf("Accept-Encoding sent = %q, want identity", sawAE)
	}
}

// TestGetObjectRangedMatchesUnrangedBytes 锁定"分块与整体下载字节一致":
// 修复前无 Range 的请求会被解压、带 Range 的不会, 同一对象两种读法结果不同。
func TestGetObjectRangedMatchesUnrangedBytes(t *testing.T) {
	stored := storedGzipObject(t)
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := stored
		if rng := r.Header.Get("Range"); rng != "" {
			// 简化: 返回同样内容的 "206" 分片, 只验证不经解压。
			w.Header().Set("Content-Range", "bytes 0-"+itoa(len(stored)-1)+"/"+itoa(len(stored)))
			w.WriteHeader(http.StatusPartialContent)
		}
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Length", itoa(len(body)))
		_, _ = w.Write(body)
	}))

	whole, err := c.GetObject(context.Background(), "bucket", "obj.gz", &GetObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = whole.Body.Close() }()
	wholeBytes, err := io.ReadAll(whole.Body)
	if err != nil {
		t.Fatal(err)
	}

	ranged, err := c.GetObject(context.Background(), "bucket", "obj.gz", &GetObjectOptions{Range: "bytes=0-"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ranged.Body.Close() }()
	rangedBytes, err := io.ReadAll(ranged.Body)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(wholeBytes, rangedBytes) {
		t.Fatalf("ranged read (%d bytes) != whole read (%d bytes); encoding handling must not depend on Range",
			len(rangedBytes), len(wholeBytes))
	}
	if !bytes.Equal(wholeBytes, stored) {
		t.Fatalf("bytes are not the stored bytes (%d vs %d)", len(wholeBytes), len(stored))
	}
}

// TestExplicitAcceptEncodingHeaderIsRespected 保留用户用 -H 显式指定时的选择权:
// 只有未指定时才注入 identity。
func TestExplicitAcceptEncodingHeaderIsRespected(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ae := r.Header.Get("Accept-Encoding"); ae != "br" {
			t.Errorf("Accept-Encoding = %q, want the user-supplied br", ae)
		}
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(http.StatusOK)
	}))
	c.extraHeaders = http.Header{"Accept-Encoding": []string{"br"}}

	resp, err := c.Do(context.Background(), http.MethodGet, requestMetadata{bucketName: "bucket", objectName: "k"})
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
}

// TestAcceptEncodingIsNotSigned 确保该头不进入 SigV4 SignedHeaders
// (它按规范属于可被中间层改写的头, 一旦被签名, 任何代理改写都会导致验签失败)。
func TestAcceptEncodingIsNotSigned(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if strings.Contains(auth, "accept-encoding") {
			t.Errorf("accept-encoding must not be in SignedHeaders: %s", auth)
		}
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(http.StatusOK)
	}))
	resp, err := c.Do(context.Background(), http.MethodGet, requestMetadata{bucketName: "bucket", objectName: "k"})
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
}
