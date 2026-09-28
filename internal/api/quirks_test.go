// quirks_test.go 验证厂商差异开关: 零值 = 严格 AWS 语义 (不改变既有行为),
// 各开关只影响自己的落点, 且互不干扰.

package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// quirksClient 用给定 Quirks 构造指向测试服务端的 client.
func quirksClient(t *testing.T, h http.Handler, q *Quirks) *Client {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	c, err := New(&Options{
		Endpoint:   s.URL,
		AccessKey:  "access",
		SecretKey:  "secret",
		MaxRetries: 1,
		Quirks:     q,
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// ---- 生命周期根元素 / 命名空间 ----

func TestQuirksLifecycleRootElement(t *testing.T) {
	var body string
	c := quirksClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.WriteHeader(http.StatusOK)
	}), &Quirks{LifecycleRootElement: "BucketLifecycleConfiguration"})

	cfg := &LifecycleConfig{Rules: []LifecycleRule{{ID: "r1", Status: "Enabled"}}}
	if err := c.SetBucketLifecycle(context.Background(), "bucket", cfg); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "<BucketLifecycleConfiguration") {
		t.Errorf("body 未使用覆盖后的根元素: %s", body)
	}

	// 零值: 仍为标准根元素
	var def string
	c2 := quirksClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		def = string(b)
		w.WriteHeader(http.StatusOK)
	}), nil)
	if err := c2.SetBucketLifecycle(context.Background(), "bucket", cfg); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(def, "<LifecycleConfiguration") || strings.Contains(def, "<BucketLifecycle") {
		t.Errorf("零值应使用标准根元素: %s", def)
	}
}

func TestQuirksXMLNSOverride(t *testing.T) {
	const custom = "https://s3.example.com/doc/2006-03-01/"
	var lifecycleBody, corsBody string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if _, ok := r.URL.Query()["lifecycle"]; ok {
			lifecycleBody = string(b)
		}
		if _, ok := r.URL.Query()["cors"]; ok {
			corsBody = string(b)
		}
		w.WriteHeader(http.StatusOK)
	})
	c := quirksClient(t, h, &Quirks{XMLNS: custom})
	ctx := context.Background()

	if err := c.SetBucketLifecycle(ctx, "bucket", &LifecycleConfig{}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetBucketCors(ctx, "bucket", &CorsConfig{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(lifecycleBody, custom) {
		t.Errorf("lifecycle xmlns 未覆盖: %s", lifecycleBody)
	}
	if !strings.Contains(corsBody, custom) {
		t.Errorf("cors xmlns 未覆盖: %s", corsBody)
	}

	// 零值: 标准命名空间 (http, 不是 https)
	var def string
	c2 := quirksClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		def = string(b)
		w.WriteHeader(http.StatusOK)
	}), nil)
	if err := c2.SetBucketCors(context.Background(), "bucket", &CorsConfig{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(def, DefaultXMLNS) {
		t.Errorf("零值应使用 %s: %s", DefaultXMLNS, def)
	}
}

// ---- Lambda 通知元素命名 (写方向开关, 读方向始终宽容) ----

func TestQuirksLambdaNotificationWrite(t *testing.T) {
	cfg := &NotificationConfiguration{
		LambdaFunctionConfigurations: []LambdaFunctionConfiguration{{ID: "l1", LambdaARN: "arn:aws:lambda:fn", Events: []string{"s3:ObjectCreated:*"}}},
	}

	cloudXML, err := marshalNotificationXML(cfg, LambdaNotificationElementCloud)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cloudXML), "<CloudFunctionConfiguration>") || !strings.Contains(string(cloudXML), "<CloudFunction>") {
		t.Errorf("cloud 命名错误: %s", cloudXML)
	}

	modernXML, err := marshalNotificationXML(cfg, LambdaNotificationElementLambda)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(modernXML), "<LambdaFunctionConfiguration>") || !strings.Contains(string(modernXML), "<LambdaFunction>") {
		t.Errorf("lambda 命名错误: %s", modernXML)
	}
	if strings.Contains(string(modernXML), "<CloudFunction>") {
		t.Errorf("lambda 命名不应残留 CloudFunction 元素: %s", modernXML)
	}
	// 通知内容不能因命名切换而丢失
	if !strings.Contains(string(modernXML), "arn:aws:lambda:fn") || !strings.Contains(string(modernXML), "<Id>l1</Id>") {
		t.Errorf("lambda 命名切换丢失内容: %s", modernXML)
	}
}

func TestNotificationUnmarshalAcceptsBothNaming(t *testing.T) {
	cases := map[string]string{
		"cloud":  `<NotificationConfiguration><CloudFunctionConfiguration><Id>l1</Id><CloudFunction>arn:cf</CloudFunction><Event>s3:ObjectCreated:*</Event></CloudFunctionConfiguration></NotificationConfiguration>`,
		"lambda": `<NotificationConfiguration><LambdaFunctionConfiguration><Id>l2</Id><LambdaFunction>arn:lf</LambdaFunction><Event>s3:ObjectRemoved:*</Event></LambdaFunctionConfiguration></NotificationConfiguration>`,
	}
	for name, raw := range cases {
		var got NotificationConfiguration
		if err := xmlDecoder(strings.NewReader(raw), &got); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(got.LambdaFunctionConfigurations) != 1 {
			t.Fatalf("%s: 应解析出 1 条 Lambda 配置, 得到 %d (%s)", name, len(got.LambdaFunctionConfigurations), raw)
		}
		if got.LambdaFunctionConfigurations[0].LambdaARN == "" {
			t.Errorf("%s: LambdaARN 未解析: %+v", name, got.LambdaFunctionConfigurations[0])
		}
	}
}

// ---- payload 哈希 ----

func TestQuirksForceUnsignedPayload(t *testing.T) {
	var sha string
	c := quirksClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sha = r.Header.Get("X-Amz-Content-Sha256")
		w.WriteHeader(http.StatusOK)
	}), &Quirks{ForceUnsignedPayload: true})

	if _, err := c.PutObject(context.Background(), "bucket", "key", []byte("hello"), nil); err != nil {
		t.Fatal(err)
	}
	if sha != unsignedPayload {
		t.Errorf("X-Amz-Content-Sha256 = %q, want %q", sha, unsignedPayload)
	}

	// 零值: 仍为真实 SHA256
	var def string
	c2 := quirksClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		def = r.Header.Get("X-Amz-Content-Sha256")
		w.WriteHeader(http.StatusOK)
	}), nil)
	if _, err := c2.PutObject(context.Background(), "bucket", "key", []byte("hello"), nil); err != nil {
		t.Fatal(err)
	}
	if def == unsignedPayload || def == "" {
		t.Errorf("零值应为真实哈希, 得到 %q", def)
	}
}

// ---- region 重定向 / 探测 ----

func TestQuirksDisableRegionRedirect(t *testing.T) {
	var calls atomic.Int32
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("X-Amz-Bucket-Region", "eu-west-1")
		w.WriteHeader(http.StatusMovedPermanently)
	})

	// 默认: 会按 X-Amz-Bucket-Region 重签重发 (两次请求后仍 301, 最终报错)
	c := quirksClient(t, h, nil)
	if _, err := c.ListObjectsV2(context.Background(), "bucket", nil); err == nil {
		t.Error("期望 301 最终失败")
	}
	if got := calls.Load(); got < 2 {
		t.Errorf("默认应重签重发, 实际请求数 %d", got)
	}

	// 关闭后: 不做第二次重签重发
	calls.Store(0)
	cq := quirksClient(t, h, &Quirks{DisableRegionRedirect: true})
	if _, err := cq.ListObjectsV2(context.Background(), "bucket", nil); err == nil {
		t.Error("期望 301 失败")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("关闭重定向后应只发 1 次请求, 实际 %d", got)
	}
}

func TestQuirksDisableRegionProbe(t *testing.T) {
	var locationHits atomic.Int32
	var lastPath string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("location") {
			locationHits.Add(1)
			_, _ = io.WriteString(w, `<LocationConstraint>eu-central-1</LocationConstraint>`)
			return
		}
		lastPath = r.URL.Path
		_, _ = io.WriteString(w, `<ListBucketResult><Name>bucket</Name></ListBucketResult>`)
	})
	newClient := func(serverURL string, q *Quirks) *Client {
		c, err := New(&Options{
			Endpoint:           serverURL,
			AccessKey:          "ak",
			SecretKey:          "sk",
			Region:             "us-east-1",
			MaxRetries:         1,
			BucketLookupViaURL: &testRegionLookup{template: serverURL + "/%(region)/%(bucket)", needRegion: true},
			Quirks:             q,
		})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}

	// 默认: 模板引用 %(region) 时先探测 ?location, 并注入探测到的 region
	s1 := httptest.NewServer(handler)
	defer s1.Close()
	if _, err := newClient(s1.URL, nil).ListObjectsV2(context.Background(), "bucket", nil); err != nil {
		t.Fatal(err)
	}
	if got := locationHits.Load(); got != 1 {
		t.Errorf("默认应探测一次 ?location, 实际 %d", got)
	}
	if !strings.Contains(lastPath, "eu-central-1") {
		t.Errorf("默认应注入探测到的 region, path = %q", lastPath)
	}

	// 关闭探测: 零次 ?location, 直接用配置 region
	locationHits.Store(0)
	lastPath = ""
	s2 := httptest.NewServer(handler)
	defer s2.Close()
	if _, err := newClient(s2.URL, &Quirks{DisableRegionProbe: true}).ListObjectsV2(context.Background(), "bucket", nil); err != nil {
		t.Fatal(err)
	}
	if got := locationHits.Load(); got != 0 {
		t.Errorf("关闭探测后应为 0 次 ?location, 实际 %d", got)
	}
	if !strings.Contains(lastPath, "us-east-1") {
		t.Errorf("关闭探测后应使用配置 region, path = %q", lastPath)
	}
}

// ---- 配置校验 ----

func TestNewRejectsInvalidQuirks(t *testing.T) {
	_, err := New(&Options{
		Endpoint:  "https://s3.example.com",
		AccessKey: "a",
		SecretKey: "b",
		Quirks:    &Quirks{LambdaNotificationElement: "lambda2"},
	})
	if err == nil {
		t.Fatal("非法 lambda_notification_element 应报错")
	}
	if !strings.Contains(err.Error(), "lambda_notification_element") {
		t.Errorf("错误信息应指出字段: %v", err)
	}
}

// TestQuirksNilEqualsZero 明确 nil Quirks 与零值 Quirks 等价 (便于上层直通配置),
// 且归一化只补默认值, 不改变语义.
func TestQuirksNilEqualsZero(t *testing.T) {
	a, err := New(&Options{Endpoint: "https://s3.example.com", AccessKey: "a", SecretKey: "b"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := New(&Options{Endpoint: "https://s3.example.com", AccessKey: "a", SecretKey: "b", Quirks: &Quirks{}})
	if err != nil {
		t.Fatal(err)
	}
	if a.quirks != b.quirks {
		t.Errorf("nil 与零值 Quirks 应等价: %+v vs %+v", a.quirks, b.quirks)
	}
	if a.quirks.LambdaNotificationElement != LambdaNotificationElementCloud {
		t.Errorf("默认 Lambda 通知命名应为 cloud: %q", a.quirks.LambdaNotificationElement)
	}
	if a.quirks.XMLNS != DefaultXMLNS {
		t.Errorf("默认命名空间应为 %s: %q", DefaultXMLNS, a.quirks.XMLNS)
	}
	if a.quirks.DisableRegionRedirect || a.quirks.DisableRegionProbe || a.quirks.ForceUnsignedPayload {
		t.Errorf("布尔开关零值必须为 false (严格语义): %+v", a.quirks)
	}
}
