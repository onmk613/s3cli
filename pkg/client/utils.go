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
	// 1. 解析路径, patherr 需要特殊解析
	s3path, patherr := utils.ParseS3Path(arg)

	// 如果error为 ErrAliasOnly，表明输入只包含 alias，不包含 bucket/key 部分
	// 这种情况下，依然尝试创建新客户端返回, 在特殊情况下有用, 比如 ls 获取 bucket 列表命令
	if patherr != nil && !errors.Is(patherr, utils.ErrAliasOnly) {
		return nil, &utils.S3Path{}, patherr
	}

	// 2. 先从缓存获取，有则直接返回
	if cachedClient, ok := S3Clients.Get(s3path.Alias); ok {
		return cachedClient, s3path, patherr
	}

	// 3. 缓存未命中，创建新客户端
	// 本身创建客户端失败了，patherr 无意义
	s3Client, err := NewS3Client(ctx, config.G.S[s3path.Alias])
	if err != nil {
		return nil, &utils.S3Path{}, err
	}

	// 4. 存入缓存，下次直接复用
	S3Clients.Set(s3path.Alias, s3Client)

	return s3Client, s3path, patherr
}
