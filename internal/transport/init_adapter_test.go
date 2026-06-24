package transport

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

// TDD RED：先写解析器测试，看到失败再实现

func TestParseTusInit_FullHeaders(t *testing.T) {
	req := httptest.NewRequest("POST", "/uploads", strings.NewReader(""))
	req.Header.Set("Upload-Length", "1024")
	req.Header.Set("X-SHA256", "abc123")
	req.Header.Set("X-Compression", "zstd")
	req.Header.Set("X-Chunk-Size", "256")
	req.Header.Set("X-File-Name", "test.bin")

	in, err := parseTusInit(req)
	if err != nil {
		t.Fatalf("parseTusInit error = %v", err)
	}
	if in.uploadLength != 1024 {
		t.Errorf("uploadLength = %d, want 1024", in.uploadLength)
	}
	if in.sha256 != "abc123" {
		t.Errorf("sha256 = %s, want abc123", in.sha256)
	}
	if in.compression != domain.CompZstd {
		t.Errorf("compression = %s, want zstd", in.compression)
	}
	if in.chunkSize != 256 {
		t.Errorf("chunkSize = %d, want 256", in.chunkSize)
	}
	if in.fileName != "test.bin" {
		t.Errorf("fileName = %s, want test.bin", in.fileName)
	}
}

func TestParseTusInit_MissingLength(t *testing.T) {
	req := httptest.NewRequest("POST", "/uploads", strings.NewReader(""))
	_, err := parseTusInit(req)
	if err == nil {
		t.Error("expected error for missing Upload-Length")
	}
}

func TestParseTusInit_NegativeLength(t *testing.T) {
	req := httptest.NewRequest("POST", "/uploads", strings.NewReader(""))
	req.Header.Set("Upload-Length", "-5")
	_, err := parseTusInit(req)
	if err == nil {
		t.Error("expected error for negative Upload-Length")
	}
}

func TestParseTusInit_DefaultCompression(t *testing.T) {
	req := httptest.NewRequest("POST", "/uploads", strings.NewReader(""))
	req.Header.Set("Upload-Length", "100")
	// 不设 X-Compression
	in, err := parseTusInit(req)
	if err != nil {
		t.Fatalf("parseTusInit error = %v", err)
	}
	if in.compression != domain.CompNone {
		t.Errorf("default compression = %s, want none", in.compression)
	}
}

func TestParseRestInit_QueryAndHeaders(t *testing.T) {
	q := url.Values{}
	q.Set("size", "2048")
	req := httptest.NewRequest("POST", "/v1/uploads/init?"+q.Encode(), strings.NewReader(""))
	req.Header.Set("X-SHA256", "rest-sha")
	req.Header.Set("X-Compression", "gzip")
	req.Header.Set("X-File-Name", "rest.txt")

	in, err := parseRestInit(req)
	if err != nil {
		t.Fatalf("parseRestInit error = %v", err)
	}
	if in.uploadLength != 2048 {
		t.Errorf("uploadLength = %d, want 2048", in.uploadLength)
	}
	if in.sha256 != "rest-sha" {
		t.Errorf("sha256 = %s", in.sha256)
	}
	if in.compression != domain.CompGzip {
		t.Errorf("compression = %s, want gzip", in.compression)
	}
	if in.chunkSize != 0 {
		t.Errorf("chunkSize = %d, REST 无此字段应为 0", in.chunkSize)
	}
}

func TestParseRestInit_MissingSize(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/uploads/init", strings.NewReader(""))
	_, err := parseRestInit(req)
	if err == nil {
		t.Error("expected error for missing size")
	}
}