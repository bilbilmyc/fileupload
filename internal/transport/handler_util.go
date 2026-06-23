package transport

import (
	"encoding/json"
	"io"
	"net/url"
	"strconv"
	"strings"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

// parseRangeHeader 解析 HTTP Range 头
// 支持格式: bytes=0-1023
func parseRangeHeader(rangeStr string) domain.DownloadRange {
	if !strings.HasPrefix(rangeStr, "bytes=") {
		return domain.DownloadRange{}
	}
	rangeStr = strings.TrimPrefix(rangeStr, "bytes=")
	parts := strings.SplitN(rangeStr, "-", 2)
	if len(parts) != 2 {
		return domain.DownloadRange{}
	}
	start, _ := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	end, _ := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
	if end > 0 && end >= start {
		return domain.DownloadRange{Offset: start, Length: end - start + 1}
	}
	return domain.DownloadRange{Offset: start}
}

// formatContentRange 格式化 Content-Range 响应头
func formatContentRange(offset, length, total int64) string {
	return "bytes " + strconv.FormatInt(offset, 10) + "-" +
		strconv.FormatInt(offset+length-1, 10) + "/" +
		strconv.FormatInt(total, 10)
}

// decodeJSON 解码 JSON 请求体
func decodeJSON(r io.Reader, v any) error {
	return json.NewDecoder(r).Decode(v)
}

// decodeFileName 对 X-File-Name 头值进行智能解码。
// 某些客户端（如浏览器）可能对 non-ASCII 文件名做了 URL 编码，兼容处理。
func decodeFileName(raw string) string {
	if raw == "" {
		return ""
	}
	// 如果包含 % 且解码后不是纯 ASCII，说明是 URL 编码的
	if strings.Contains(raw, "%") {
		if decoded, err := url.QueryUnescape(raw); err == nil && decoded != raw {
			return decoded
		}
	}
	return raw
}
