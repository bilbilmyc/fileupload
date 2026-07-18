package compressor

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

// zipArchiver zip 归档写入器（仅作为 Archiver 注册；zip 不支持单流 Decompress）。
type zipArchiver struct{}

func (zipArchiver) Format() domain.CompressionFormat { return domain.CompZip }

func (zipArchiver) NewArchiveWriter(_ context.Context, w io.Writer) (domain.ArchiveWriter, error) {
	return &zipArchiveWriter{zw: zip.NewWriter(w)}, nil
}

func init() { RegisterArchiver(zipArchiver{}) }

// zipArchiveWriter 包装 zip.Writer 实现 domain.ArchiveWriter。
type zipArchiveWriter struct {
	zw *zip.Writer
}

func (w *zipArchiveWriter) AddFile(_ context.Context, name string, size int64, content io.Reader) error {
	hdr := &zip.FileHeader{
		Name:   name,
		Method: zip.Deflate,
	}
	hdr.SetMode(0644)
	hdr.Modified = time.Now()

	fw, err := w.zw.CreateHeader(hdr)
	if err != nil {
		return fmt.Errorf("创建 zip 条目 %s: %w", name, err)
	}
	if _, err := io.Copy(fw, content); err != nil {
		return fmt.Errorf("写入 zip 条目 %s: %w", name, err)
	}
	return nil
}

func (w *zipArchiveWriter) Close() error {
	return w.zw.Close()
}
