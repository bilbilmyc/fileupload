package testutil

import (
	"context"
	"io"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

// MockCompressor implements domain.Compressor
type MockCompressor struct{}

// NewMockCompressor 创建 MockCompressor
func NewMockCompressor() *MockCompressor { return &MockCompressor{} }

func (m *MockCompressor) Decompress(_ context.Context, r io.Reader, _ domain.CompressionFormat) (io.ReadCloser, error) {
	return io.NopCloser(r), nil
}

func (m *MockCompressor) NewArchiveWriter(_ context.Context, w io.Writer, _ domain.CompressionFormat) (domain.ArchiveWriter, error) {
	return &MockArchiveWriter{w: w}, nil
}

// MockArchiveWriter implements domain.ArchiveWriter
type MockArchiveWriter struct{ w io.Writer }

func (m *MockArchiveWriter) AddFile(_ context.Context, _ string, _ int64, r io.Reader) error {
	_, err := io.Copy(m.w, r)
	return err
}

func (m *MockArchiveWriter) Close() error { return nil }
