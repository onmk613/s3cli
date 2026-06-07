package utils

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// 设计上alias和bucket靠冒号分隔，不允许出现冒号。

// ErrAliasOnly 表示输入只包含 alias，没有 bucket/key 部分。
// 调用方可以通过 errors.Is(err, ErrAliasOnly) 来判断并做特殊处理。
var ErrAliasOnly = errors.New("alias only: no bucket or key specified")

// AliasNameRegex 校验 alias 名
var AliasNameRegex = regexp.MustCompile(`^[A-Za-z0-9][a-zA-Z0-9\_\-\.]{1,63}[A-Za-z0-9]$`)

// BucketNameRegex 校验 bucket 名
var BucketNameRegex = regexp.MustCompile(`^[A-Za-z0-9][a-zA-Z0-9\_\-\.]{1,61}[A-Za-z0-9]$`)

// S3Path 表示一条 "alias:bucket/key" 形态的解析结果。
type S3Path struct {
	Alias         string // alias 名 (必填)
	Bucket        string // bucket 名 (必填)
	Key           string // object key, 可为空; 多级路径会保留中间的 "/"
	TrailingSlash bool   // 原始输入是否以 "/" 结尾 (用于区分 "目录" / "对象")
}

// ParseS3Path 解析 "alias:bucket/key" 格式的路径。
//
//	"mys3:mybucket"              -> {Alias:"mys3", Bucket:"mybucket"}
//	"mys3:mybucket/"             -> {Alias:"mys3", Bucket:"mybucket"}
//	"mys3:mybucket/a/b.txt"      -> {Alias:"mys3", Bucket:"mybucket", Key:"a/b.txt"}
//	"mys3:mybucket/dir/"         -> {Alias:"mys3", Bucket:"mybucket", Key:"dir/", TrailingSlash:true}
//
// 返回的 Key 不含前导 "/", 也不含尾部 "/" (尾部 "/" 信息通过 TrailingSlash 表达)。
func ParseS3Path(s string) (*S3Path, error) {
	if s == "" {
		return nil, fmt.Errorf("empty s3 path")
	}

	// 1. 切 alias
	colon := strings.Index(s, ":")
	if colon < 0 {
		// 没有冒号: 整个字符串视为 alias_name
		if !AliasNameRegex.MatchString(s) {
			return nil, fmt.Errorf("invalid alias name %q: must match %s", s, AliasNameRegex.String())
		}
		return &S3Path{Alias: s}, ErrAliasOnly
	}

	if colon == 0 {
		return nil, fmt.Errorf("invalid s3 path %q: expected alias:bucket[/key]", s)
	}
	alias := s[:colon]
	rest := s[colon+1:]

	if !AliasNameRegex.MatchString(alias) {
		return nil, fmt.Errorf("invalid alias name %q: must match %s", alias, AliasNameRegex.String())
	}

	if rest == "" {
		return nil, fmt.Errorf("invalid s3 path %q: bucket is required after %q:", s, alias)
	}

	// 2. 切 bucket / key
	var bucket, key string
	trailing := strings.HasSuffix(rest, "/")
	if slash := strings.Index(rest, "/"); slash >= 0 {
		bucket = rest[:slash]
		key = rest[slash+1:]
	} else {
		bucket = rest
	}

	if !BucketNameRegex.MatchString(bucket) {
		return nil, fmt.Errorf("invalid bucket name %q: must match %s", bucket, BucketNameRegex.String())
	}

	// 去掉尾部 "/", 但保留中间分隔符
	key = strings.TrimSuffix(key, "/")

	if key == "" {
		trailing = false
	}

	// TrailingSlash 时把 "/" 加回 key，确保前缀匹配精确
	if trailing {
		key = key + "/"
	}

	return &S3Path{
		Alias:         alias,
		Bucket:        bucket,
		Key:           key,
		TrailingSlash: trailing,
	}, nil
}

// NormalizePrefix 统一处理 s3:// 路径中的 prefix 后缀斜杠逻辑：
//   - 如果 path 以 "/" 结尾且 key 非空，追加 "/"（列出目录内容）
//   - 兼容 ceph: "//" 规范化回 "/"
func NormalizePrefix(path, key string) string {
	prefix := key
	if strings.HasSuffix(path, "/") && key != "" {
		prefix = key + "/"
	}
	if prefix == "//" {
		prefix = "/"
	}
	return prefix
}

// ResolveDestKey 计算 cp/mv 操作的目标 key。
//
// destPath 以 "/" 结尾 -> 目标视为目录，将 srcBase 拼接到 destKey 下。
// destKey 为空 -> 直接用 srcBase。
func ResolveDestKey(destPath, destKey, srcBase string) string {
	if strings.HasSuffix(destPath, "/") {
		if destKey == "" {
			return srcBase
		}
		if !strings.HasSuffix(destKey, "/") {
			destKey += "/"
		}
		return destKey + srcBase
	}
	if destKey == "" {
		return srcBase
	}
	return destKey
}
