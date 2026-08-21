// bucket-lookup.go 处理 bucket 寻址方式 (path-style / virtual-host-style / 自定义模板),
// 将 (bucket, object, query) 解析为最终请求 URL. 当自定义模板引用 %(region) 占位符时,
// 会通过 GetBucketLocation 探测 bucket 的实际 region (带进程内缓存).

package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// BucketLookupType 表示 bucket 的寻址方式.
type BucketLookupType int

const (
	// BucketLookupAuto 自动选择寻址方式; 未提供自定义模板时默认退化为 BucketLookupPath.
	BucketLookupAuto BucketLookupType = iota
	// BucketLookupDNS 虚拟主机风格: http://<bucket>.<host>/<object>.
	BucketLookupDNS
	// BucketLookupPath 路径风格: http://<host>/<bucket>/<object>.
	BucketLookupPath
)

// BucketLookupFunc 由自定义寻址模板实现, 决定如何把 (bucket, region) 解析为基准 endpoint.
// 当模板引用了 %(region) 占位符时, Client 会先探测 bucket 的实际 region 再调用 ResolveCustomEndpoint.
type BucketLookupFunc interface {
	// NeedsRegion 表示模板是否引用了 region 占位符 (如 %(region))。
	// 为 true 时, Client 会解析 bucket 的实际 region (带缓存探测) 再注入模板;
	// 为 false 时直接用配置 region, 零网络开销 (默认方案)。
	NeedsRegion() bool
	ResolveCustomEndpoint(bucket, region string) (*url.URL, error)
}

// resolveURL 根据 bucket 寻址方式 (path / virtual-host / 自定义模板) 生成目标 URL.
func (c *Client) resolveURL(ctx context.Context, bucketName, objectName string, queryValues url.Values) (*url.URL, error) {
	// 自定义 endpoint 模板: bucket 已由模板注入到 host 或 path 中,
	// 这里仅在其基础上追加 object key 与 query, 不再重复注入 bucket,
	// 否则会出现 "bucket 同时存在于 host 和 path" 的双重寻址错误。
	if c.lookupFn != nil {
		region := c.region
		// 仅当模板引用了 %(region) 时才解析 bucket 的实际 region (带缓存探测);
		// 否则用配置 region, 避免不必要的网络请求。
		if c.lookupFn.NeedsRegion() {
			region = c.resolveBucketRegion(ctx, bucketName)
		}
		base, err := c.lookupFn.ResolveCustomEndpoint(bucketName, region)
		if err != nil {
			return nil, err
		}
		// 运行期二次校验: 配置期 validateCustomTemplate 只用固定测试值 (如
		// "test-bucket") 校验模板, 真实 bucket 名 (如含 "..") 替换进模板后可能
		// 拼出非法 host, 这里在构造请求 URL 前校验。
		if err := validateResolvedCustomEndpoint(base, bucketName); err != nil {
			return nil, err
		}
		return buildURLFromBase(base, objectName, queryValues)
	}

	// 没有定义lookupFn，且lookup为BucketLookupAuto时，默认使用BucketLookupPath寻址方式
	lookup := c.lookup
	if lookup == BucketLookupAuto {
		lookup = BucketLookupPath
	}
	return c.buildURL(c.endpointURL.Host, c.endpointURL.Scheme, c.endpointURL.Path, bucketName, objectName, queryValues, lookup)
}

// resolveBucketRegion 解析 bucket 的 region: 优先读缓存, 未命中则探测 GetBucketLocation 并写缓存。
// 探测失败或返回空 (如 us-east-1 / 厂商不支持) 时回退到配置 region。
// 每个 bucket 在进程内最多探测一次, 避免每请求都做探针。
func (c *Client) resolveBucketRegion(ctx context.Context, bucket string) string {
	if v, ok := c.bucketLocCache.Get(bucket); ok {
		return v
	}
	region := c.region
	if loc, err := c.probeBucketLocation(ctx, bucket); err == nil && loc != "" {
		region = loc
	}
	c.bucketLocCache.Set(bucket, region)
	return region
}

// probeBucketLocation 通过基准 endpoint (path-style) 查询 bucket 的 region,
// 用于自定义寻址模板含 %(region) 时解析 region。
// 强制 path-style 以打破 "需要 region 才能寻址、寻址才能拿 region" 的引导环。
func (c *Client) probeBucketLocation(ctx context.Context, bucket string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	urlValues := make(url.Values)
	urlValues.Set("location", "")

	resp, err := c.Do(ctx, http.MethodGet, requestMetadata{
		bucketName:     bucket,
		queryValues:    urlValues,
		forcePathStyle: true,
	})
	if err != nil {
		return "", err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	var result getBucketLocationResult
	if err := xmlDecoder(resp.Body, &result); err != nil {
		return "", err
	}
	return result.LocationConstraint, nil
}

// validateResolvedCustomEndpoint 校验自定义寻址模板输出的 URL (此时占位符已替换为
// 真实 bucket / region)。失败返回带 bucket 名与模板输出信息的明确错误。
func validateResolvedCustomEndpoint(u *url.URL, bucketName string) error {
	raw := u.String()
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("custom endpoint template produced invalid URL for bucket %q: %v", bucketName, err)
	}
	if parsed.Host == "" || strings.Contains(parsed.Host, "..") {
		return fmt.Errorf("custom endpoint template produced invalid URL for bucket %q: %q (host must be non-empty and must not contain \"..\")", bucketName, raw)
	}
	return nil
}

// buildURL 根据寻址方式拼接最终请求 URL.
// host/scheme/basePath 为基准地址 (basePath 是 endpoint 的路径前缀, 如网关
// "https://gw.example.com/s3" 的 "/s3", 不参与 bucket 注入但必须保留, 否则
// path/DNS 寻址对带路径前缀的 endpoint 全部 404); DNS 风格会把 bucket 前置到
// host, path 风格把 bucket 放入路径.
func (c *Client) buildURL(host, scheme, basePath, bucketName, objectName string, queryValues url.Values, lookup BucketLookupType) (*url.URL, error) {
	// 统一为 "/a/b" 形态 (无尾斜杠, 可为空), 拼接时再补 "/"
	basePath = strings.Trim(basePath, "/")
	var p string
	switch {
	case bucketName == "":
		// service 级请求, 如 ListBuckets
		p = "/"
		if basePath != "" {
			p = "/" + basePath + "/"
		}
	case lookup == BucketLookupDNS:
		host = bucketName + "." + host
		p = "/"
		if basePath != "" {
			p = "/" + basePath + "/"
		}
		if objectName != "" {
			p += objectName
		}
	default: // BucketLookupPath
		p = "/" + bucketName
		if basePath != "" {
			p = "/" + basePath + p
		}
		if objectName != "" {
			p += "/" + objectName
		}
		// objectName 为空时保持 "/test-bucket"，不加尾斜杠
	}

	u := &url.URL{
		Scheme: scheme,
		Host:   host,
		Path:   p,
		// Path must remain unescaped. RawPath is the matching escaped form used
		// by url.URL and SigV4; assigning an already escaped path to Path makes
		// '%' become '%25' on the wire.
		RawPath: encodePath(p),
	}
	if len(queryValues) > 0 {
		u.RawQuery = s3EncodeQuery(queryValues)
	}
	return u, nil
}

// buildURLFromBase 在自定义寻址模板解析出的 base URL 上追加 object key 与 query。
// base 已包含 bucket (由模板注入到 host 或 path), 故此处不再重复注入 bucket,
// 避免 "bucket 同时出现在 host 与 path" 的双重寻址错误。
func buildURLFromBase(base *url.URL, objectName string, queryValues url.Values) (*url.URL, error) {
	u := *base // 浅拷贝: 复用 scheme/host/user 等, 仅重写 path 与 query

	// 去掉 base 末尾斜杠后追加 object; 保留 bucket 由模板决定的位置。
	p := strings.TrimRight(u.Path, "/")
	if objectName != "" {
		p += "/" + objectName
	}
	if p == "" {
		p = "/"
	}

	u.Path = p
	// Path 保持未转义, RawPath 为 SigV4 所需的转义形式 (与 buildURL 一致)。
	u.RawPath = encodePath(p)
	if len(queryValues) > 0 {
		u.RawQuery = s3EncodeQuery(queryValues)
	}
	return &u, nil
}
