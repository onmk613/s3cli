package action

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"strings"
	"sync"

	"s3cli/pkg/utils"

	"github.com/aws/smithy-go"
)

// IsCanceled 判断 err 是否由用户主动取消（Ctrl+C / SIGTERM）或超时引起。
//
// 主动取消会让所有在途请求返回 context.Canceled（有时被 SDK 包成 RequestCanceled
// 等 API 错误）。这类错误不应被当作真正的传输/比对失败上报，调用方据此静默跳过。
func IsCanceled(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "RequestCanceled", "RequestCanceledError":
			return true
		}
	}
	// 兜底：部分路径会把 context.Canceled 文本包进普通 error，丢失了可 Is 的链。
	return strings.Contains(err.Error(), "context canceled")
}

func FormatAPIError(err error) string {
	if err == nil {
		return ""
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return fmt.Sprintf("%s: %s", apiErr.ErrorCode(), apiErr.ErrorMessage())
	}
	return err.Error()
}

// FormatBytes 委托给 fmtutil.FormatBytes
func FormatBytes(bytes int64) string {
	return utils.FormatBytes(bytes)
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
