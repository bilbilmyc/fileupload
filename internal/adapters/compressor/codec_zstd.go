package compressor

import (
	"archive/tar"
	"context"
	"fmt"
	"io"

	"github.com/bilbilmyc/fileupload/internal/domain"
	"github.com/klauspost/compress/zstd"
)

// zstdCodec 处理 zstd 与 tar.zst。
type zstdCodec struct{}

func (zstdCodec) Format() domain.CompressionFormat { return domain.CompZstd }

// zstd 流式解压（异步管道，避免一次性读完整流）。
func (zstdCodec) Decompress(_ context.Context, r io.Reader) (io.ReadCloser, error) {
	pr, pw := io.Pipe()
	go func() {
		dec, err := zstd.NewReader(r)
		if err != nil {
			pw.CloseWithError(fmt.Errorf("创建 zstd reader: %w", err))
			return
		}
		defer dec.Close()
		_, err = io.Copy(pw, dec)
		pw.CloseWithError(err)
	}()
	return pr, nil
}

func init() { RegisterDecompressor(zstdCodec{}) }

// ============================================================
// tar.zst Archiver
// ============================================================

type zstdArchiver struct{}

func (zstdArchiver) Format() domain.CompressionFormat { return domain.CompTarZst }

func (zstdArchiver) NewArchiveWriter(_ context.Context, w io.Writer) (domain.ArchiveWriter, error) {
	zw, err := zstd.NewWriter(w)
	if err != nil {
		return nil, fmt.Errorf("创建 zstd writer: %w", err)
	}
	tw := tar.NewWriter(zw)
	return newTarArchiveWriter(tw, zw, domain.CompTarZst), nil
}

func init() { RegisterArchiver(zstdArchiver{}) }

// tarZstDecompressor 同步版 zstd 解压（用于 tar.zst 流，避免与单流 zstd 共享异步管道）。
type tarZstDecompressor struct{}

func (tarZstDecompressor) Format() domain.CompressionFormat { return domain.CompTarZst }

func (tarZstDecompressor) Decompress(_ context.Context, r io.Reader) (io.ReadCloser, error) {
	dec, err := zstd.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("创建 zstd reader: %w", err)
	}
	return dec.IOReadCloser(), nil
}

func init() { RegisterDecompressor(tarZstDecompressor{}) }
