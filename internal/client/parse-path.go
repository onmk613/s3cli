package client

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/onmk613/s3cli/internal/config"
	"github.com/onmk613/s3cli/internal/s3path"
	"github.com/onmk613/s3cli/pkg/api"
	"github.com/onmk613/s3cli/pkg/kvcache"
)

// cachedBackend 缓存项: 已构造的后端客户端 + 构造它时使用的静态配置。
// 命中缓存时会比对静态配置: 同一 Resolver 生命周期内别名配置变化
// 时自动重建, 不再返回陈旧凭证的客户端。
type cachedBackend struct {
	client *api.Client
	static config.Static
}

// Resolver 持有「一次命令执行」范围内的客户端缓存, 负责把 alias 解析为
// 已构造的后端客户端。
//
// 缓存作用域刻意限制在一次命令执行内, 而不是进程级全局变量:
//   - 单条命令内重复解析同一 alias 很常见 (多参数命令、cp/mirror 的源与目标
//     同属一个 alias、diff 的两个端点)。复用同一个 http.Transport 可以省掉
//     重复的 TCP/TLS 握手, 这是缓存真正的收益; 单纯"少构造一个对象"只有
//     微秒级, 不值得为之引入全局状态;
//   - 命令结束即释放, 凭证/连接池/配置不会残留在包级变量里, 测试之间也
//     不会通过共享缓存互相污染;
//   - 由于不跨命令存活, 无需处理 flags 变化导致的失效 —— CLI 进程内
//     flags 只解析一次。
//
// 底层 kvcache 并发安全; 当前所有解析都在命令层串行完成, 但这样即使将来
// 在批处理 worker 内解析路径也无需额外加锁。
type Resolver struct {
	cache kvcache.Cache[string, cachedBackend]
}

// NewResolver 创建一次命令执行使用的客户端解析器。
func NewResolver() *Resolver { return &Resolver{} }

// New 直接构造一个后端客户端, 不做缓存。供补全等一次性场景使用。
func New(static config.Static) (*api.Client, error) {
	return newBackendClient(static, config.G.F)
}

// Client 返回 alias 对应的后端客户端; 同一静态配置在同一个 Resolver 内
// 只构造一次。底层实现为自建请求的 api.Client, 调用方不感知具体实现。
func (r *Resolver) Client(alias string, static config.Static) (*api.Client, error) {
	if cached, ok := r.cache.Get(alias); ok && reflect.DeepEqual(cached.static, static) {
		return cached.client, nil
	}

	s3Client, err := New(static)
	if err != nil {
		return nil, err
	}

	r.cache.Set(alias, cachedBackend{client: s3Client, static: static})
	return s3Client, nil
}

// ParsePath 解析 S3 路径并返回对应的后端客户端。
func (r *Resolver) ParsePath(arg string) (*api.Client, *s3path.Path, error) {
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

	s3Client, clientErr := r.Client(p.Alias, static)
	if clientErr != nil {
		return nil, nil, clientErr
	}

	return s3Client, p, err
}
