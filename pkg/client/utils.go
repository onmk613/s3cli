package client

import (
	"context"
	"errors"
	"s3cli/pkg/config"
	"s3cli/pkg/kvcache"
	"s3cli/pkg/utils"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

var S3Clients = &kvcache.Cache[string, *s3.Client]{}

func ParsePathAndNewClient(ctx context.Context, arg string) (*s3.Client, *utils.S3Path, error) {
	s3path, patherr := utils.ParseS3Path(arg)

	// 如果error为 ErrAliasOnly，表明输入只包含 alias，不包含 bucket/key 部分
	if patherr != nil && !errors.Is(patherr, utils.ErrAliasOnly) {
		return nil, &utils.S3Path{}, patherr
	}

	if cachedClient, ok := S3Clients.Get(s3path.Alias); ok {
		return cachedClient, s3path, patherr
	}

	s3Client, err := NewS3Client(ctx, config.G.S[s3path.Alias])
	if err != nil {
		return nil, &utils.S3Path{}, err
	}

	S3Clients.Set(s3path.Alias, s3Client)
	return s3Client, s3path, patherr
}
