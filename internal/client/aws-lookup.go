//go:build aws

// aws-lookup.go 为官方 SDK 后端 (awss3.AWS) 实现与 s3api 等价的 bucket 寻址:
// path-style (UsePathStyle) / virtual-host (默认) / 自定义占位符模板.
//
// 自定义模板通过实现 s3.EndpointResolverV2 完成: 模板中的 %(bucket) / %(region)
// 占位符在解析阶段替换, 与 SDK 默认规则集的寻址行为一致 (bucket 注入 host 或 path,
// 请求路径由 finalize 阶段拼接). 当模板引用 %(region) 时, 与 s3api 相同会通过
// GetBucketLocation 探测 bucket 的实际 region (带进程内缓存, 探测走 path-style
// 基准 endpoint, 打破 "需要 region 才能寻址" 的引导环).

package client

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	smithyauth "github.com/aws/smithy-go/auth"
	smithyendpoints "github.com/aws/smithy-go/endpoints"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// templateEndpointResolver 用占位符模板解析 S3 endpoint, 实现 s3.EndpointResolverV2.
type templateEndpointResolver struct {
	template string
	bucketPh string
	regionPh string

	cfgRegion string
	// probeOpts 构造 path-style 探测客户端 (GetBucketLocation) 所需的选项, 延迟初始化.
	probeOpts s3.Options

	mu          sync.Mutex
	regionCache map[string]string
	probeOnce   sync.Once
	probeClient *s3.Client
	probeErr    error
}

// newTemplateEndpointResolver 构造自定义寻址解析器.
// tpl 含 %(bucket) (及可选 %(region)) 占位符; probeOpts 用于 region 探测.
func newTemplateEndpointResolver(tpl, bucketPh, regionPh, cfgRegion string, probeOpts s3.Options) *templateEndpointResolver {
	return &templateEndpointResolver{
		template:    tpl,
		bucketPh:    bucketPh,
		regionPh:    regionPh,
		cfgRegion:   cfgRegion,
		probeOpts:   probeOpts,
		regionCache: make(map[string]string),
	}
}

// NeedsRegion 报告模板是否引用了 region 占位符.
func (r *templateEndpointResolver) NeedsRegion() bool {
	return r.regionPh != "" && strings.Contains(r.template, r.regionPh)
}

// ResolveEndpoint 把模板中的占位符替换为请求的 bucket / region, 返回最终 endpoint.
// 与 s3api 的 ResolveCustomEndpoint 语义一致; 模板引用 %(region) 时会先探测
// bucket 的实际 region.
func (r *templateEndpointResolver) ResolveEndpoint(ctx context.Context, params s3.EndpointParameters) (smithyendpoints.Endpoint, error) {
	bucket := aws.ToString(params.Bucket)
	if bucket == "" && strings.Contains(r.template, r.bucketPh) {
		return smithyendpoints.Endpoint{}, fmt.Errorf("bucket is required for custom addressing")
	}

	region := r.cfgRegion
	if r.NeedsRegion() {
		region = r.resolveRegion(ctx, bucket)
	}

	raw := strings.ReplaceAll(r.template, r.bucketPh, bucket)
	if r.regionPh != "" {
		raw = strings.ReplaceAll(raw, r.regionPh, region)
	}

	u, err := url.Parse(raw)
	if err != nil {
		return smithyendpoints.Endpoint{}, fmt.Errorf("parse custom endpoint %q: %w", raw, err)
	}
	if u.Host == "" {
		return smithyendpoints.Endpoint{}, fmt.Errorf("custom endpoint %q has empty host", raw)
	}

	return smithyendpoints.Endpoint{
		URI:     *u,
		Headers: make(map[string][]string),
		Properties: func() smithy.Properties {
			var out smithy.Properties
			smithyauth.SetAuthOptions(&out, []*smithyauth.Option{
				{
					SchemeID: "sigv4",
					SignerProperties: func() smithy.Properties {
						var sp smithy.Properties
						smithyhttp.SetDisableDoubleEncoding(&sp, true)
						smithyhttp.SetSigV4SigningName(&sp, "s3")
						smithyhttp.SetSigV4SigningRegion(&sp, region)
						return sp
					}(),
				},
			})
			return out
		}(),
	}, nil
}

// resolveRegion 解析 bucket 的实际 region: 优先读缓存, 未命中则用 path-style
// 基准 endpoint 探测 GetBucketLocation 并写缓存; 探测失败或为空时回退配置 region.
func (r *templateEndpointResolver) resolveRegion(ctx context.Context, bucket string) string {
	r.mu.Lock()
	if v, ok := r.regionCache[bucket]; ok {
		r.mu.Unlock()
		return v
	}
	r.mu.Unlock()

	region := r.cfgRegion
	client, err := r.probe()
	if err == nil {
		if out, err := client.GetBucketLocation(ctx, &s3.GetBucketLocationInput{Bucket: aws.String(bucket)}); err == nil {
			if loc := string(out.LocationConstraint); loc != "" {
				region = loc
			}
		}
	}

	r.mu.Lock()
	r.regionCache[bucket] = region
	r.mu.Unlock()
	return region
}

// probe 延迟构造用于 GetBucketLocation 探测的 path-style 客户端.
func (r *templateEndpointResolver) probe() (*s3.Client, error) {
	r.probeOnce.Do(func() {
		opts := r.probeOpts
		opts.UsePathStyle = true
		r.probeClient = s3.New(opts)
	})
	return r.probeClient, r.probeErr
}
