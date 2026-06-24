package compressor

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

// gzipCodec 处理 gzip 与 tar.gz：单流 Decompress 剥离 gzip；tar.gz 还作为归档写入器。
type gzipCodec struct{}

func (gzipCodec) Format() domain.CompressionFormat { return domain.CompGzip }

// gzip / tar.gz 共用 Decompress：仅剥离外层 gzip，不解 tar。
func (gzipCodec) Decompress(_ context.Context, r io.Reader) (io.ReadCloser, error) {
	return gzip.NewReader(r)
}

func init() { RegisterDecompressor(gzipCodec{}) }

// ============================================================
// tar.gz Archiver
// ============================================================

type gzipArchiver struct{}

func (gzipArchiver) Format() domain.CompressionFormat { return domain.CompTarGz }

func (gzipArchiver) NewArchiveWriter(_ context.Context, w io.Writer) (domain.ArchiveWriter, error) {
	gw := gzip.NewWriter(w)
	tw := tar.NewWriter(gw)
	return newTarArchiveWriter(tw, gw, domain.CompTarGz), nil
}

func init() { RegisterArchiver(gzipArchiver{}) }

// ============================================================
// 共享 tar 写入器（供 gzip / zstd 的归档写入器共用）
// ============================================================

type tarArchiveWriter struct {
	tw  *tar.Writer
	cw  io.Closer
	fmt domain.CompressionFormat
}

func newTarArchiveWriter(tw *tar.Writer, cw io.Closer, format domain.CompressionFormat) *tarArchiveWriter {
	return &tarArchiveWriter{tw: tw, cw: cw, fmt: format}
}

func (w *tarArchiveWriter) AddFile(_ context.Context, name string, size int64, content io.Reader) error {
	hdr := &tar.Header{
		Name:     name,
		Size:     size,
		Typeflag: tar.TypeReg,
		Mode:     0644,
	}
	if err := w.tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("写入 tar header: %w", err)
	}
	if _, err := io.Copy(w.tw, content); err != nil {
		return fmt.Errorf("写入 tar body: %w", err)
	}
	return nil
}

func (w *tarArchiveWriter) Close() error {
	if err := w.tw.Close(); err != nil {
		return fmt.Errorf("关闭 tar writer: %w", err)
	}
	if err := w.cw.Close(); err != nil {
		return fmt.Errorf("关闭压缩 writer: %w", err)
	}
	return nil
}

// 防止 time 包被 unused import 检测报错的占位。
// 实际 zipArchiver 需要 time.Now()，这里只是给 gzip/tarArchiver 编译用。