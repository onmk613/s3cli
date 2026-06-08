package client

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"s3cli/pkg/httptracer"
	"time"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"s3cli/pkg/config"
)

// NewS3Client 根据配置构建一个全局可用的 S3 客户端
func NewS3Client(ctx context.Context, cfg config.Static) (*s3.Client, error) {
	// 构建 HTTP 客户端，设置合理的超时和连接池参数，并根据调试/SSL 配置启用相应功能
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !cfg.IsVerifySSL()},
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   100,
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
