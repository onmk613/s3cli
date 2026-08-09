package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// 文件系统与编码钩子（测试注入用）：让 saveConfig 的错误分支可被稳定触发。
var (
	mkdirAll   = os.MkdirAll
	createTemp = os.CreateTemp
	chmodFile  = (*os.File).Chmod
	encodeTOML = tomlEncode
	syncFile   = (*os.File).Sync
	closeFile  = (*os.File).Close
	rename     = os.Rename
	chmodPath  = os.Chmod
)

func tomlEncode(w io.Writer, v any) error {
	return toml.NewEncoder(w).Encode(v)
}

// saveConfig 以原子方式写入整张别名表，并设置仅限所有者访问的权限。
// 输入是 alias 名 → Static 的整张表；内部按 buildOutputMap 过滤后用 TOML 编码。
func saveConfig(confPath string) error {
	aliases := G.S
	out := make(map[string]map[string]any, len(aliases))
	for name, s := range aliases {
		out[name] = buildOutputMap(s)
	}

	dir := filepath.Dir(confPath)
	if err := mkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	tmp, err := createTemp(dir, ".s3cli-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if err := chmodFile(tmp, 0o600); err != nil {
		_ = closeFile(tmp)
		return err
	}
	if err := encodeTOML(tmp, out); err != nil {
		_ = closeFile(tmp)
		return fmt.Errorf("encode config: %w", err)
	}
	if err := syncFile(tmp); err != nil {
		_ = closeFile(tmp)
		return err
	}
	if err := closeFile(tmp); err != nil {
		return err
	}
	if err := rename(tmpName, confPath); err != nil {
		return err
	}
	return chmodPath(confPath, 0o600)
}

// buildOutputMap 把单个别名转换为 map[string]any，跳过取默认值的字段，
// 让写入的配置文件保持简洁。
// 字段裁剪规则：
//   - access_key / secret_key / host_base 必写
//   - session_token / region / bucket_lookup / default_mime_type: 非空才写
//   - no_verify_ssl: 仅 true 才写 (缺省 = false)
//   - multipart_chunk_size_mb: > 0 且 != 15 才写
//   - max_retries: > 0 才写
func buildOutputMap(s Static) map[string]any {
	m := map[string]any{
		"access_key": s.AccessKey,
		"secret_key": s.SecretKey,
		"host_base":  s.HostBase,
	}
	if s.SessionToken != "" {
		m["session_token"] = s.SessionToken
	}
	if s.Region != "" {
		m["region"] = s.Region
	}
	if s.DefaultMimeType != "" {
		m["default_mime_type"] = s.DefaultMimeType
	}
	if s.NoVerifySSL {
		m["no_verify_ssl"] = true
	}
	if s.BucketLookup != "" {
		m["bucket_lookup"] = s.BucketLookup
	}
	if s.MultipartChunkSizeMb > 0 && s.MultipartChunkSizeMb != DefaultPartSizeMB {
		m["multipart_chunk_size_mb"] = s.MultipartChunkSizeMb
	}
	if s.MaxRetries > 0 {
		m["max_retries"] = s.MaxRetries
	}
	return m
}
