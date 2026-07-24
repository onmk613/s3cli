package client

import (
	"context"
	"errors"
	"fmt"
	"s3cli/internal/config"
	"s3cli/internal/s3path"
	"s3cli/pkg/kvcache"
	"s3cli/pkg/s3api"
)

var S3Clients = &kvcache.Cache[string, *s3api.Client]{}

func ParsePathAndNewClient(ctx context.Context, arg string) (*s3api.Client, *s3path.Path, error) {
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

	s3Client, err := NewS3Client(ctx, static, config.G.F)
	if err != nil {
		return nil, &s3path.Path{}, err
	}

	S3Clients.Set(sp.Alias, s3Client)
	return s3Client, sp, path
}
