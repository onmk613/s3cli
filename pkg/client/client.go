// Package client 负责构建唯一的全局 S3 客户端
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

func NewS3Client(ctx context.Context, cfg config.Static) (*s3.Client, error) {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !cfg.IsVerifySSL()},
	}
	var rt http.RoundTripper = transport

	if cfg.IsDebug() {
		rt = httptracer.New(transport, nil)
	}
	httpClient := &http.Client{
		Transport: rt,
	}

	c, err := awscfg.LoadDefaultConfig(
		ctx,
		awscfg.WithRegion(cfg.GetRegion()),
		awscfg.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.GetAccessKey(), cfg.GetSecretKey(), cfg.GetSessionToken())),
		awscfg.WithHTTPClient(httpClient),
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
