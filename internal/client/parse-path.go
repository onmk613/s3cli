package client

import (
	"errors"
	"fmt"
	"reflect"
	"s3cli/internal/config"
	"s3cli/internal/s3path"
	"s3cli/pkg/kvcache"
	"s3cli/pkg/s3iface"
)

// cachedBackend 缓存项: 已构造的后端客户端 + 构造它时使用的静态配置。
// 命中缓存时会比对静态配置: 同一进程内别名配置变化 (如未来的交互式/常驻模式)
// 时自动重建, 不再返回陈旧凭证的客户端。
type cachedBackend struct {
	client s3iface.S3Operations
	static config.Static
}

// S3Clients 缓存已构造的 S3 后端客户端 (按 alias), 避免重复构造.
var S3Clients = &kvcache.Cache[string, cachedBackend]{}

// NewClient 按 alias 的静态配置构造编译期选定的 S3 后端客户端,
// 返回 s3iface.S3Operations 接口. 底层实现为自建请求的 api.Client,
// 调用方不感知具体实现.
func NewClient(alias string, static config.Static) (s3iface.S3Operations, error) {
	if cached, ok := S3Clients.Get(alias); ok && reflect.DeepEqual(cached.static, static) {
		return cached.client, nil
	}

	s3Client, err := newBackendClient(static, config.G.F)
	if err != nil {
		return nil, err
	}

	S3Clients.Set(alias, cachedBackend{client: s3Client, static: static})
	return s3Client, nil
}

func ParsePathAndNewClient(arg string) (s3iface.S3Operations, *s3path.Path, error) {
	p, err := s3path.Parse(arg)

	// ErrAliasOnly 表明输入只包含 alias，不包含 bucket/key 部分：
	// 客户端依然可用，err 作为"仅别名"语义返回给调用方判断 (errors.Is)。
	if err != nil && !errors.Is(err, s3path.ErrAliasOnly) {
		return nil, nil, err
	}

	// alias 不存在时给出明确指引, 而不是零值配置导致的
	// "endpoint, access key, and secret key cannot be empty" 误导性报错。
	static, ok := config.G.S[p.Alias]
	if !ok {
		return nil, nil, fmt.Errorf("alias %q not found in config %s (run `s3cli alias set %s URL ACCESSKEY SECRETKEY` to create, `s3cli alias list` to see existing)",
			p.Alias, config.G.C, p.Alias)
	}

	s3Client, clientErr := NewClient(p.Alias, static)
	if clientErr != nil {
		return nil, nil, clientErr
	}

	return s3Client, p, err
}
