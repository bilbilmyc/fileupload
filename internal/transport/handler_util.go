package transport

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/url"
	"strconv"
	"strings"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

// parseRangeHeader parses a single RFC 7233 byte range. Multiple and suffix ranges are
// intentionally rejected because the service only streams one continuous reader.
func parseRangeHeader(rangeStr string) (domain.DownloadRange, error) {
	if rangeStr == "" {
		return domain.DownloadRange{}, nil
	}
	if !strings.HasPrefix(rangeStr, "bytes=") || strings.Contains(rangeStr, ",") {
		return domain.DownloadRange{}, domain.ErrInvalidArgument
	}
	value := strings.TrimSpace(strings.TrimPrefix(rangeStr, "bytes="))
	parts := strings.SplitN(value, "-", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" && !strings.HasSuffix(value, "-") {
		return domain.DownloadRange{}, domain.ErrInvalidArgument
	}
	start, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil || start < 0 {
		return domain.DownloadRange{}, domain.ErrInvalidArgument
	}
	rng := domain.DownloadRange{Offset: start, Requested: true}
	endStr := strings.TrimSpace(parts[1])
	if endStr == "" {
		return rng, nil
	}
	end, err := strconv.ParseInt(endStr, 10, 64)
	if err != nil || end < start {
		return domain.DownloadRange{}, domain.ErrInvalidArgument
	}
	rng.Length = end - start + 1
	return rng, nil
}

// contentDisposition returns an RFC 5987-safe header and prevents quote/newline injection
// through untrusted file names.
func contentDisposition(disposition, fileName string) string {
	value := mime.FormatMediaType(disposition, map[string]string{"filename": fileName})
	if value == "" {
		return fmt.Sprintf(`%s; filename="download"`, disposition)
	}
	return value
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
