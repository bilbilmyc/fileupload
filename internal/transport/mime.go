package transport

import "strings"

// guessMimeType 根据文件名猜测 MIME 类型
func guessMimeType(name string) string {
	dotIdx := strings.LastIndex(name, ".")
	if dotIdx < 0 || dotIdx >= len(name)-1 {
		return "application/octet-stream"
	}
	ext := strings.ToLower(name[dotIdx+1:])
	switch ext {
	// 图片
	case "jpg", "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	case "svg":
		return "image/svg+xml"
	case "bmp":
		return "image/bmp"
	case "ico":
		return "image/x-icon"
	// 文档
	case "pdf":
		return "application/pdf"
	case "txt", "log", "md":
		return "text/plain; charset=utf-8"
	case "html", "htm":
		return "text/html; charset=utf-8"
	case "json":
		return "application/json; charset=utf-8"
	case "xml":
		return "application/xml; charset=utf-8"
	case "csv":
		return "text/csv; charset=utf-8"
	case "yaml", "yml":
		return "text/yaml; charset=utf-8"
	// 代码
	case "js", "jsx", "ts", "tsx":
		return "text/plain; charset=utf-8"
	case "go", "py", "rb", "rs", "java", "c", "cpp", "h", "hpp", "cs", "swift", "kt":
		return "text/plain; charset=utf-8"
	case "sh", "bash", "zsh", "ps1", "bat":
		return "text/plain; charset=utf-8"
	case "css", "scss", "less":
		return "text/plain; charset=utf-8"
	case "sql":
		return "text/plain; charset=utf-8"
	// 视频
	case "mp4":
		return "video/mp4"
	case "webm":
		return "video/webm"
	case "avi":
		return "video/x-msvideo"
	case "mov":
		return "video/quicktime"
	case "mkv":
		return "video/x-matroska"
	// 音频
	case "mp3":
		return "audio/mpeg"
	case "wav":
		return "audio/wav"
	case "ogg", "oga":
		return "audio/ogg"
	case "flac":
		return "audio/flac"
	case "aac":
		return "audio/aac"
	case "m4a":
		return "audio/mp4"
	// 字体
	case "woff", "woff2":
		return "font/" + ext
	case "ttf":
		return "font/ttf"
	case "otf":
		return "font/otf"
	default:
		return "application/octet-stream"
	}
}
