package client

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"s3cli/pkg/httptracer"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"s3cli/pkg/config"
)

// 构建 S3 客户端
func NewS3Client(ctx context.Context, cfg config.Static) (*s3.Client, error) {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !cfg.IsVerifySSL()},
	}
	var rt http.RoundTripper = transport

	if cfg.IsDebug() {
		rt = httptracer.New(rt, nil)
	}

	// User-Agent 改写放在最外层: 先改写请求, 再交给(可能存在的)tracer dump,
	// 这样 --debug 输出里看到的就是改写后的最终 User-Agent。
	rt = newUserAgentTransport(rt, cfg.GetUserAgent(), cfg.GetUserAgentSuffix())

	// 自定义 HTTP header 注入(放在最外层, --debug 可见最终请求头)。
	customHeaders, err := parseHeaders(cfg.GetHeaders())
	if err != nil {
		return nil, err
	}
	rt = newCustomHeaderTransport(rt, customHeaders)

	httpClient := &http.Client{
		Transport: rt,
	}

	c, err := awscfg.LoadDefaultConfig(
		ctx,
		awscfg.WithRegion(cfg.GetRegion()),
		awscfg.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.GetAccessKey(), cfg.GetSecretKey(), cfg.GetSessionToken())),
		awscfg.WithHTTPClient(httpClient),
		// aws-sdk-go-v2 默认 WhenSupported, 会在 Put/UploadPart 上强制添加
		// CRC32 trailer 并使用 aws-chunked 流式编码; 不少非 AWS 网关(如部分
		// 版本的 SeaweedFS/MinIO)无法识别该 trailer, 导致上传立即失败。
		// 改为 WhenRequired, 仅在调用方显式要求时才计算/校验, 提升兼容性。
		awscfg.WithRequestChecksumCalculation(awsv2.RequestChecksumCalculationWhenRequired),
		awscfg.WithResponseChecksumValidation(awsv2.ResponseChecksumValidationWhenRequired),
	)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	lookup, customTpl, err := cfg.ResolveBucketLookup()
	if err != nil {
		return nil, err
	}

	S3 := s3.NewFromConfig(c, func(o *s3.Options) {
		o.BaseEndpoint = awsv2.String(cfg.GetEndpoint())

		switch lookup {
		case config.BucketLookupPath:
			o.UsePathStyle = true
		case config.BucketLookupDNS:
			o.UsePathStyle = false
		case config.BucketLookupCustom:
			// 自定义寻址: 用 EndpointResolverV2 按模板拼出 endpoint,
			// 此时 BaseEndpoint 不再生效, 由 resolver 全权决定.
			o.BaseEndpoint = nil
			o.UsePathStyle = false
			o.EndpointResolverV2 = &customBucketResolver{
				Template:          customTpl,
				BucketPlaceholder: config.BucketPlaceholder,
			}
		}
	})

	return S3, nil
}
