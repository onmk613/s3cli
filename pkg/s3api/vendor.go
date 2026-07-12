package s3api

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"
)

type Provider string

// ProviderCapabilities records behavior that differs across S3-compatible
// implementations. Unknown endpoints deliberately receive the conservative
// AWS-compatible defaults until a dedicated provider profile is added.
type ProviderCapabilities struct {
	SupportsSigV4            bool
	SupportsVirtualHostStyle bool
	SupportsObjectLock       bool
	SupportsBucketQuota      bool
}

const (
	ProviderAws       Provider = "aws"
	ProviderMinIO     Provider = "minio"
	ProviderSeaweedFS Provider = "seaweedfs"
)

var providerCapabilities = map[Provider]ProviderCapabilities{
	ProviderAws:       {SupportsSigV4: true, SupportsVirtualHostStyle: true, SupportsObjectLock: true},
	ProviderMinIO:     {SupportsSigV4: true, SupportsVirtualHostStyle: true, SupportsObjectLock: true, SupportsBucketQuota: true},
	ProviderSeaweedFS: {SupportsSigV4: true, SupportsVirtualHostStyle: true},
}

// Capabilities returns a stable, conservative compatibility profile.
func (c *Client) Capabilities() ProviderCapabilities {
	if capabilities, ok := providerCapabilities[c.vendor]; ok {
		return capabilities
	}
	return providerCapabilities[ProviderAws]
}

// detectVendor 通过发送一个轻量级 HEAD 请求探测 S3 厂商类型.
// 根据 Server 响应头判断
// 探测失败或无法识别时默认返回 aws.
// 该探测对用户无感知: 仅发一次请求, 3 秒超时, 失败静默回退.
func detectVendor(ctx context.Context, endpoint, accessKey, secretKey, sessionToken, region string, transport http.RoundTripper) Provider {
	if ctx == nil {
		ctx = context.Background()
	}
	// 探测请求设 3 秒超时, 避免阻塞客户端初始化
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	// 构建一个临时 Client 用于探测 (复用传入的 transport)
	// NotCheckVendor=true 避免递归: 探测 Client 不再触发 detectVendor
	probeClient, err := New(&Options{
		Endpoint:       endpoint,
		AccessKey:      accessKey,
		SecretKey:      secretKey,
		SessionToken:   sessionToken,
		Region:         region,
		Transport:      transport,
		MaxRetries:     0,           // 探测不重试
		Vendor:         ProviderAws, // 探测 Client 的默认厂商
		NotCheckVendor: true,        // 避免递归探测
	})
	if err != nil {
		return ProviderAws
	}

	// 发送 ListBuckets 请求, 只关心响应头
	resp, err := probeClient.Do(ctx, http.MethodGet, requestMetadata{})
	if err != nil {
		return ProviderAws
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	return classifyByServerHeader(resp.Header.Get("Server"))
}

// classifyByServerHeader 根据 Server 响应头识别厂商.
func classifyByServerHeader(server string) Provider {
	s := strings.ToLower(strings.TrimSpace(server))
	switch {
	case strings.Contains(s, "minio"):
		return ProviderMinIO
	default:
		return ProviderAws
	}
}
