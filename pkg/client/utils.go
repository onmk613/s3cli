package client

import (
	"context"
	"errors"
	"s3cli/pkg/config"
	"s3cli/pkg/kvcache"
	"s3cli/pkg/s3api"
	"s3cli/pkg/utils"
)

var S3Clients = &kvcache.Cache[string, *s3api.Client]{}

func ParsePathAndNewClient(ctx context.Context, arg string) (*s3api.Client, *utils.S3Path, error) {
	s3path, path := utils.ParseS3Path(arg)

	// 如果error为 ErrAliasOnly，表明输入只包含 alias，不包含 bucket/key 部分
	if path != nil && !errors.Is(path, utils.ErrAliasOnly) {
		return nil, &utils.S3Path{}, path
	}

	if cachedClient, ok := S3Clients.Get(s3path.Alias); ok {
		return cachedClient, s3path, path
	}

	s3Client, err := NewS3Client(ctx, config.G.S[s3path.Alias])
	if err != nil {
		return nil, &utils.S3Path{}, err
	}

	S3Clients.Set(s3path.Alias, s3Client)
	return s3Client, s3path, path
}
