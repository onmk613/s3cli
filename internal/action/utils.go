// utils.go 提供 action 包内复用的通用工具: 取消判断 (IsCanceled)、
// 校验和算法解析 (parseChecksumAlg)、MIME 类型注册 (AddMime)。
// 字节单位换算与 JSON 输出分别由 fmtutil 与 action/render 提供。

package action

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"sync"

	"s3cli/internal/api"
	"s3cli/internal/i18n"
)

// IsCanceled 判断 err 是否由用户主动取消（Ctrl+C / SIGTERM）或超时引起。
//
// 只依赖 errors.Is: 全代码库的错误包装统一使用 %w, context.Canceled 一定
// 留在错误链上, 不再需要字符串匹配兜底。
func IsCanceled(err error) bool {
	if err == nil {
		return false
	}
	// 注意: DeadlineExceeded (超时) 不是用户取消 —— 以 130 静默退出会吞掉超时错误,
	// 超时应正常报错并退出 1。
	return errors.Is(err, context.Canceled)
}

// parseChecksumAlg 解析 put/get 的 --checksum 取值; 空串经
// api.ParseChecksumAlgorithm 归一为不启用。两个命令共用同一份报错文案。
func parseChecksumAlg(v string) (api.ChecksumAlgorithm, error) {
	alg, ok := api.ParseChecksumAlgorithm(v)
	if !ok {
		return "", fmt.Errorf(i18n.T("unsupported checksum algorithm %q (expected CRC32 / CRC32C / SHA1 / SHA256)", "不支持的校验和算法 %q（可选 CRC32 / CRC32C / SHA1 / SHA256）"), v)
	}
	return alg, nil
}

var addMimeOnce sync.Once

func AddMime() {
	addMimeOnce.Do(func() {
		entries := map[string]string{
			".mp4": "video/mp4", ".webm": "video/webm", ".ogv": "video/ogg",
			".avi": "video/x-msvideo", ".mpeg": "video/mpeg", ".mov": "video/quicktime",
			".flv": "video/x-flv", ".wmv": "video/x-ms-wmv", ".mkv": "video/x-matroska",

			".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
			".gif": "image/gif", ".bmp": "image/bmp", ".tiff": "image/tiff", ".tif": "image/tiff",
			".svg": "image/svg+xml", ".webp": "image/webp", ".avif": "image/avif", ".ico": "image/x-icon",

			".mp3": "audio/mpeg", ".wav": "audio/wav", ".ogg": "audio/ogg",
			".flac": "audio/flac", ".aac": "audio/aac", ".m4a": "audio/mp4",
			".aiff": "audio/aiff", ".aif": "audio/aiff", ".mid": "audio/midi",
			".midi": "audio/midi", ".opus": "audio/opus",

			".pdf": "application/pdf", ".doc": "application/msword",
			".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
			".xls":  "application/vnd.ms-excel",
			".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
			".ppt":  "application/vnd.ms-powerpoint",
			".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
			".odt":  "application/vnd.oasis.opendocument.text",
			".ods":  "application/vnd.oasis.opendocument.spreadsheet",
			".odp":  "application/vnd.oasis.opendocument.presentation",
			".rtf":  "application/rtf", ".txt": "text/plain", ".csv": "text/csv",
			".html": "text/html", ".htm": "text/html", ".json": "application/json",
			".xml": "application/xml", ".md": "text/markdown", ".epub": "application/epub+zip",

			".zip": "application/zip", ".tar": "application/x-tar",
			".gz": "application/gzip", ".tgz": "application/gzip",
			".bz2": "application/x-bzip2", ".xz": "application/x-xz",
			".rar": "application/vnd.rar", ".7z": "application/x-7z-compressed",

			".js":  "application/javascript",
			".mjs": "application/javascript",
			".ts":  "application/typescript",
			".css": "text/css", ".scss": "text/x-scss",
			".php": "application/x-httpd-php", ".py": "text/x-script.python",
			".java": "text/x-java-source", ".c": "text/x-c", ".cpp": "text/x-c++",
			".h": "text/x-c", ".hpp": "text/x-c++", ".go": "text/x-go", ".rs": "text/x-rust",
			".sh": "application/x-sh", ".yaml": "application/yaml", ".yml": "application/yaml",
			".toml": "application/toml",
		}
		for ext, ct := range entries {
			_ = mime.AddExtensionType(ext, ct)
		}
	})
}
