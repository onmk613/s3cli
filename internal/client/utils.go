package client

import (
	"context"
	"errors"
	"fmt"
	"s3cli/internal/config"
	"s3cli/internal/s3path"
	"s3cli/pkg/kvcache"
	"s3cli/pkg/s3iface"
)

// S3Clients 缓存已构造的 S3 后端客户端 (按 alias), 避免重复构造.
var S3Clients = &kvcache.Cache[string, s3iface.S3Operations]{}

// NewClient 按 alias 的静态配置构造 S3 后端客户端, 返回 s3iface.S3Operations 接口.
// 后端选择: CLI --backend 优先, 其次 alias 配置的 backend 字段; "aws" 用官方 SDK
// (awss3.AWS), 其余默认自建请求 (s3api.Client). 调用方不感知具体实现.
func NewClient(ctx context.Context, alias string, static config.Static) (s3iface.S3Operations, error) {
	if cachedClient, ok := S3Clients.Get(alias); ok {
		return cachedClient, nil
	}

	backend := config.G.F.Backend
	if backend == "" {
		backend = static.Backend
	}

	var s3Client s3iface.S3Operations
	var err error
	if backend == "aws" {
		s3Client, err = NewAWSClient(ctx, static, config.G.F)
	} else {
		s3Client, err = NewS3Client(ctx, static, config.G.F)
	}
	if err != nil {
		return nil, err
	}

	S3Clients.Set(alias, s3Client)
	return s3Client, nil
}

func ParsePathAndNewClient(ctx context.Context, arg string) (s3iface.S3Operations, *s3path.Path, error) {
	sp, path := s3path.Parse(arg)

	// 如果error为 ErrAliasOnly，表明输入只包含 alias，不包含 bucket/key 部分
	if path != nil && !errors.Is(path, s3path.ErrAliasOnly) {
		return nil, &s3path.Path{}, path
	}

	if cachedClient, ok := S3Clients.Get(sp.Alias); ok {
		return cachedClient, sp, path
	}

	// alias 不存在时给出明确指引, 而不是零值配置导致的
	// "endpoint, access key, and secret key cannot be empty" 误导性报错。
	static, ok := config.G.S[sp.Alias]
	if !ok {
		return nil, &s3path.Path{}, fmt.Errorf("alias %q not found in config %s (run `s3cli alias set %s` to create, or `s3cli alias list` to see existing)", sp.Alias, config.ConfPath, sp.Alias)
	}

	s3Client, err := NewClient(ctx, sp.Alias, static)
	if err != nil {
		return nil, &s3path.Path{}, err
	}

	return s3Client, sp, path
}
