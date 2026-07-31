//go:build aws

// awsclient.go 构建官方 AWS SDK 后端 (awss3.AWS), 作为自建 s3api.Client 的编译期替代.
//
// 本文件由 build tag "aws" 控制: 加 -tags aws 构建时, 唯一编译进二进制的后端为 awss3.AWS
// (见 backend-aws.go).

package client

import (
	"context"
	"crypto/tls"
	"net/http"

	"s3cli/internal/config"
	"s3cli/pkg/awss3"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// NewAWSClient 根据别名静态配置构造 awss3.AWS (官方 SDK 后端).
func NewAWSClient(ctx context.Context, cfg config.Static, flags config.Flags) (*awss3.AWS, error) {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: !cfg.VerifySSL},
	}
	var rt http.RoundTripper = transport

	if flags.Debug {
		rt = NewDumper(rt)
	}
	rt = newUserAgentTransport(rt, flags.UserAgent, flags.UserAgentSuffix)
	customHeaders, err := parseHeaders(flags.Headers)
	if err != nil {
		return nil, err
	}
	rt = newCustomHeaderTransport(rt, customHeaders)

	region := cfg.Region
	if region == "" {
		region = "us-east-1"
	}

	maxRetries := cfg.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}

	lookup, customTpl, err := cfg.ResolveBucketLookup()
	if err != nil {
		return nil, err
	}

	opts := s3.Options{
		BaseEndpoint:     aws.String(cfg.HostBase),
		Region:           region,
		Credentials:      credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, cfg.SessionToken),
		HTTPClient:       &http.Client{Transport: rt},
		RetryMaxAttempts: maxRetries,
		// aws-sdk-go-v2 默认 WhenSupported, 会在 Put/UploadPart 上强制添加
		// CRC32 trailer 并使用 aws-chunked 流式编码; 不少非 AWS 网关(如部分
		// 版本的 SeaweedFS/MinIO)无法识别该 trailer, 导致上传立即失败。
		// 改为 WhenRequired, 仅在调用方显式要求时才计算/校验, 提升兼容性。
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	}

	// bucket 寻址: 与 s3api 后端保持一致语义.
	//  - path:  强制 path-style (bucket 放入路径), 通用性最好;
	//  - dns:   virtual-host 风格 (bucket 前置到 host); SDK 对 IP/localhost
	//           endpoint 会自动回退 path-style;
	//  - custom: 自定义占位符模板 (见 aws-lookup.go 的 templateEndpointResolver).
	switch lookup {
	case config.BucketLookupPath:
		opts.UsePathStyle = true
	case config.BucketLookupDNS:
		// 默认即为 virtual-host 风格, 无需额外配置
	case config.BucketLookupCustom:
		opts.EndpointResolverV2 = newTemplateEndpointResolver(
			customTpl, config.BucketPlaceholder, config.RegionPlaceholder, region, opts,
		)
	}

	s3Client := s3.New(opts)

	return awss3.NewAWS(ctx, s3Client)
}
