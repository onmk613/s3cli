// Package utils 提供路径解析、AWS 配置文件加载等基础工具函数。
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
	TrailingSlash bool   // 原始输入是否以 "/" 结尾 (用于区分 "DIR" / "OBJECT")
}

// ParseS3Path 解析 "alias:bucket/key" 格式的路径
func ParseS3Path(s string) (*S3Path, error) {
	if s == "" {
		return nil, fmt.Errorf("empty s3 path")
	}

	colon := strings.Index(s, ":")
	if colon < 0 {
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

// DestState 描述目标路径在对象存储中的当前状态。
type DestState int

const (
	DestNone DestState = iota // 目标不存在
	DestDir                   // 目标存在且为目录前缀
	DestFile                  // 目标存在且为文件对象
)

// trimSlash 去掉首尾的 "/"。
func trimSlash(s string) string {
	return strings.Trim(s, "/")
}

// ResolveFileDest 计算「单个文件」源的目标对象 key
func ResolveFileDest(destKey string, destTrailing bool, srcBase string) string {
	dest := trimSlash(destKey)
	if destTrailing {
		if dest == "" {
			return srcBase
		}
		return dest + "/" + srcBase
	}
	if dest == "" {
		return srcBase
	}
	return dest
}

// ResolveDirDestPrefix 计算「目录」源的目标前缀及是否需要追加相对路径
func ResolveDirDestPrefix(srcKey string, srcTrailing bool, destKey string, destTrailing bool, state DestState) (destPrefix string, appendRel bool) {
	dest := trimSlash(destKey)
	srcBase := lastSegment(srcKey)

	if destTrailing {
		if srcTrailing {
			return dest, true
		}
		// 源无尾斜杠：把源目录名追加到目标前缀下
		return joinNonEmpty(dest, srcBase), true
	}

	// 目标无尾斜杠
	switch state {
	case DestFile:
		return dest, false
	case DestDir:
		return dest, true
	default: // DestNone
		if srcTrailing {
			return dest, false
		}
		return dest, true
	}
}

// lastSegment 返回路径最后一段（去尾斜杠后取 base）。
func lastSegment(p string) string {
	p = trimSlash(p)
	if p == "" {
		return ""
	}
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// joinNonEmpty 用 "/" 连接两段，忽略空串。
func joinNonEmpty(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	default:
		return a + "/" + b
	}
}
