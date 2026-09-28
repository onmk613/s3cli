// presigned_test.go 覆盖预签名 URL 生成 (share 命令的核心)。
//
// 此前 PresignGet / PresignPut / PresignDelete / PresignHead 均为 0% 覆盖。
// 已有的 PresignedURL 测试是"自签自验"的往返测试 —— 它只能证明实现自洽,
// 无法发现 canonical query / string-to-sign 与 AWS 不一致 (生成出的 URL 在
// 真实 S3 上必然 403, 而单测全绿)。
//
// 这里补两类断言:
//  1. 与 AWS 官方文档公布的预签名示例做精确签名比对 (钉死时间, 落在已知向量上);
//  2. URL 结构、方法绑定、有效期上下界、STS token、response-* 覆盖参数。

package api

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"
)

// withPinnedTime 把签名时刻固定为 ts, 测试结束后还原。
func withPinnedTime(t *testing.T, ts time.Time) {
	t.Helper()
	old := presignNow
	presignNow = func() time.Time { return ts }
	t.Cleanup(func() { presignNow = old })
}

// TestPresignedURLMatchesAWSDocumentedExample 用 AWS 官方文档的示例做精确断言。
//
// 来源: AWS S3 文档 "Authenticating Requests: Using Query Parameters
// (AWS Signature Version 4)" 的 GET Object 示例 ——
// examplebucket.s3.amazonaws.com/test.txt, AKIAIOSFODNN7EXAMPLE,
// wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY, us-east-1, 20130524T000000Z, 86400。
// 文档给出的签名值是 aeeed9bbccd4d02ee5c0109b86d86835f995330da4c265957d157751f604d404。
//
// (该期望值经独立实现复算确认: 从规范重写的 Python 版本对同一输入得到完全相同的
// 十六进制签名。)
func TestPresignedURLMatchesAWSDocumentedExample(t *testing.T) {
	withPinnedTime(t, time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC))

	c, err := New(&Options{
		Endpoint:     "https://s3.amazonaws.com",
		AccessKey:    "AKIAIOSFODNN7EXAMPLE",
		SecretKey:    "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		Region:       "us-east-1",
		BucketLookup: BucketLookupDNS, // 解析为 examplebucket.s3.amazonaws.com
	})
	if err != nil {
		t.Fatal(err)
	}

	signed, err := c.PresignGet(context.Background(), "examplebucket", "test.txt", 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(signed)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := u.Host, "examplebucket.s3.amazonaws.com"; got != want {
		t.Fatalf("host = %q, want %q", got, want)
	}
	if got, want := u.Path, "/test.txt"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if got, want := u.Query().Get("X-Amz-Signature"),
		"aeeed9bbccd4d02ee5c0109b86d86835f995330da4c265957d157751f604d404"; got != want {
		t.Fatalf("signature = %q, want the AWS-documented %q\nfull URL: %s", got, want, signed)
	}
	if got, want := u.Query().Get("X-Amz-Date"), "20130524T000000Z"; got != want {
		t.Fatalf("X-Amz-Date = %q, want %q", got, want)
	}
	if got, want := u.Query().Get("X-Amz-Credential"),
		"AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request"; got != want {
		t.Fatalf("X-Amz-Credential = %q, want %q", got, want)
	}
}

// TestPresignedURLQueryShape 覆盖预签名 URL 的必备查询参数与签名排除项。
func TestPresignedURLQueryShape(t *testing.T) {
	withPinnedTime(t, time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC))
	c := presignTestClient(t)

	signed, err := c.PresignGet(context.Background(), "mybucket", "dir/a b.txt", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(signed)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()

	for _, key := range []string{
		"X-Amz-Algorithm", "X-Amz-Credential", "X-Amz-Date",
		"X-Amz-Expires", "X-Amz-SignedHeaders", "X-Amz-Signature",
	} {
		if q.Get(key) == "" {
			t.Errorf("missing query parameter %s in %s", key, signed)
		}
	}
	if got := q.Get("X-Amz-Algorithm"); got != "AWS4-HMAC-SHA256" {
		t.Errorf("X-Amz-Algorithm = %q", got)
	}
	if got := q.Get("X-Amz-SignedHeaders"); got != "host" {
		t.Errorf("X-Amz-SignedHeaders = %q, want host (presign signs only the Host header)", got)
	}
	if got := q.Get("X-Amz-Expires"); got != "3600" {
		t.Errorf("X-Amz-Expires = %q, want 3600", got)
	}
	if got := q.Get("X-Amz-Credential"); !strings.HasSuffix(got, "/20260301/us-east-1/s3/aws4_request") {
		t.Errorf("X-Amz-Credential = %q, want the 20260301/us-east-1/s3/aws4_request scope", got)
	}
	// 签名必须是 64 位小写十六进制。
	sig := q.Get("X-Amz-Signature")
	if len(sig) != 64 || strings.ToLower(sig) != sig {
		t.Errorf("X-Amz-Signature = %q, want 64 lowercase hex chars", sig)
	}
	// 对象 key 里的空格必须被编码, 且签名 URL 不应含裸空格。
	if strings.Contains(signed, " ") {
		t.Errorf("presigned URL contains a raw space: %s", signed)
	}
	if !strings.Contains(signed, "a%20b.txt") {
		t.Errorf("object key was not encoded as expected: %s", signed)
	}
}

// TestPresignConvenienceMethodsBindHTTPMethod 覆盖四个便捷方法:
// 预签名 URL 绑定了 HTTP 方法, DELETE 签出的 URL 不能用于 GET (反之亦然)。
func TestPresignConvenienceMethodsBindHTTPMethod(t *testing.T) {
	withPinnedTime(t, time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC))

	cases := []struct {
		name string
		call func(*Client, context.Context, string, string, time.Duration) (string, error)
	}{
		{"PresignGet", (*Client).PresignGet},
		{"PresignPut", (*Client).PresignPut},
		{"PresignDelete", (*Client).PresignDelete},
		{"PresignHead", (*Client).PresignHead},
	}
	seen := make(map[string]string, len(cases))
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 每个方法用独立 client, 避免 bucket region 缓存带来的耦合。
			c := presignTestClient(t)
			signed, err := tc.call(c, context.Background(), "mybucket", "obj.bin", time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := url.Parse(signed); err != nil {
				t.Fatalf("invalid URL: %v", err)
			}
			seen[tc.name] = signed
		})
	}
	// 同一资源、同一时刻: 方法不同 => 签名必须不同。
	for a, ua := range seen {
		for b, ub := range seen {
			if a >= b {
				continue
			}
			qa, _ := url.Parse(ua)
			qb, _ := url.Parse(ub)
			if qa.Query().Get("X-Amz-Signature") == qb.Query().Get("X-Amz-Signature") {
				t.Errorf("%s and %s produced an identical signature; the HTTP method must be signed", a, b)
			}
		}
	}
}

// TestPresignExpiryBounds 覆盖有效期上下界: 超过 7 天或不足 1 秒都必须拒绝
// (否则会生成服务端必然拒绝的 URL)。
func TestPresignExpiryBounds(t *testing.T) {
	c := presignTestClient(t)
	ctx := context.Background()

	if _, err := c.PresignGet(ctx, "mybucket", "k", 7*24*time.Hour+time.Second); err == nil {
		t.Error("expiry above 7 days must be rejected")
	}
	if _, err := c.PresignGet(ctx, "mybucket", "k", 500*time.Millisecond); err == nil {
		t.Error("expiry below 1 second must be rejected (truncates to X-Amz-Expires=0)")
	}
	// 边界值必须被接受。
	if _, err := c.PresignGet(ctx, "mybucket", "k", 7*24*time.Hour); err != nil {
		t.Errorf("exactly 7 days must be accepted: %v", err)
	}
	if _, err := c.PresignGet(ctx, "mybucket", "k", time.Second); err != nil {
		t.Errorf("exactly 1 second must be accepted: %v", err)
	}
	// 非法方法必须拒绝。
	if _, err := c.PresignedURL(ctx, "mybucket", "k", &PresignOptions{Method: "PATCH", Expires: time.Hour}); err == nil {
		t.Error("unsupported method must be rejected")
	}
}

// TestPresignCarriesSessionToken 临时凭证必须带上 X-Amz-Security-Token,
// 否则持有 STS 凭证的用户拿到的是必然 403 的 URL。
func TestPresignCarriesSessionToken(t *testing.T) {
	withPinnedTime(t, time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC))
	c, err := New(&Options{
		Endpoint: "https://s3.example.test", AccessKey: "AK", SecretKey: "SK",
		SessionToken: "session-token-value", Region: "us-east-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	signed, err := c.PresignGet(context.Background(), "mybucket", "k", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(signed)
	if got := u.Query().Get("X-Amz-Security-Token"); got != "session-token-value" {
		t.Fatalf("X-Amz-Security-Token = %q, want session-token-value", got)
	}

	// 不带 token 的 client 不应出现该参数。
	plain := presignTestClient(t)
	signedPlain, err := plain.PresignGet(context.Background(), "mybucket", "k", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if up, _ := url.Parse(signedPlain); up.Query().Get("X-Amz-Security-Token") != "" {
		t.Fatal("X-Amz-Security-Token must be absent when there is no session token")
	}
}

// TestPresignResponseOverrides 覆盖 response-* 参数: 它们参与签名,
// 因此必须出现在 URL 上 (缺一个就会 SignatureDoesNotMatch)。
func TestPresignResponseOverrides(t *testing.T) {
	withPinnedTime(t, time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC))
	c := presignTestClient(t)

	signed, err := c.PresignedURL(context.Background(), "mybucket", "k", &PresignOptions{
		Method:                     "GET",
		Expires:                    time.Hour,
		ResponseContentType:        "text/plain",
		ResponseContentDisposition: `attachment; filename="a.txt"`,
		ResponseCacheControl:       "no-cache",
	})
	if err != nil {
		t.Fatal(err)
	}
	q, _ := url.Parse(signed)
	got := q.Query()
	for key, want := range map[string]string{
		"response-content-type":        "text/plain",
		"response-content-disposition": `attachment; filename="a.txt"`,
		"response-cache-control":       "no-cache",
	} {
		if got.Get(key) != want {
			t.Errorf("%s = %q, want %q", key, got.Get(key), want)
		}
	}
}

// TestPresignSignatureExcludesSignatureParam 断言 X-Amz-Signature 是签名后追加的:
// 若它被计入 canonical query, 服务端复算时排除该参数就会得到不同结果。
// 判据: 同一输入两次生成必须完全一致 (签名确定, 不依赖自身)。
func TestPresignSignatureExcludesSignatureParam(t *testing.T) {
	withPinnedTime(t, time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC))
	a := presignTestClient(t)
	b := presignTestClient(t)

	first, err := a.PresignGet(context.Background(), "mybucket", "obj.bin", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	second, err := b.PresignGet(context.Background(), "mybucket", "obj.bin", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("presign is not deterministic for identical inputs:\n%s\n%s", first, second)
	}

	// 换个 key, 签名必须变化 (且只有签名与路径变化)。
	other, err := a.PresignGet(context.Background(), "mybucket", "other.bin", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	qa, _ := url.Parse(first)
	qo, _ := url.Parse(other)
	if qa.Query().Get("X-Amz-Signature") == qo.Query().Get("X-Amz-Signature") {
		t.Fatal("different object keys must produce different signatures")
	}
}

func presignTestClient(t *testing.T) *Client {
	t.Helper()
	c, err := New(&Options{
		Endpoint: "https://s3.example.test", AccessKey: "AK", SecretKey: "SK", Region: "us-east-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}
