// Package compressor 实现 domain.Compressor 端口的压缩/解压/归档适配器
package compressor

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"

	"github.com/klauspost/compress/zstd"
	"github.com/bilbilmyc/fileupload/internal/domain"
)

// Compressor 压缩/解压/归档实现
// 支持格式：zstd、gzip、tar.gz、tar.zst、zip
type Compressor struct {
	zstdDec *zstd.Decoder
	zstdEnc *zstd.Encoder
}

// NewCompressor 创建压缩器（带 zstd 编解码器池化）
func NewCompressor() (*Compressor, error) {
	dec, err := zstd.NewReader(nil)
	if err != nil {
		return nil, fmt.Errorf("初始化 zstd decoder: %w", err)
	}
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		dec.Close()
		return nil, fmt.Errorf("初始化 zstd encoder: %w", err)
	}
	return &Compressor{
		zstdDec: dec,
		zstdEnc: enc,
	}, nil
}

// Decompress 解压：输入压缩流，输出原始数据流
func (c *Compressor) Decompress(_ context.Context, r io.Reader, format domain.CompressionFormat) (io.ReadCloser, error) {
	switch format {
	case domain.CompZstd:
		// zstd 流式解压
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

	case domain.CompGzip, domain.CompTarGz:
		return gzip.NewReader(r)

	case domain.CompTarZst:
		// tar.zst = zstd 流解压
		dec, err := zstd.NewReader(r)
		if err != nil {
			return nil, fmt.Errorf("创建 zstd reader: %w", err)
		}
		return dec.IOReadCloser(), nil

	case domain.CompNone:
		return io.NopCloser(r), nil

	default:
		return nil, fmt.Errorf("不支持的压缩格式: %s", format)
	}
}

// NewArchiveWriter 创建流式归档写入器
func (c *Compressor) NewArchiveWriter(_ context.Context, w io.Writer, format domain.CompressionFormat) (domain.ArchiveWriter, error) {
	switch format {
	case domain.CompTarGz:
		gw := gzip.NewWriter(w)
		tw := tar.NewWriter(gw)
		return &tarArchiveWriter{
			tw:  tw,
			cw:  gw,
			fmt: format,
		}, nil

	case domain.CompTarZst:
		zw, err := zstd.NewWriter(w)
		if err != nil {
			return nil, fmt.Errorf("创建 zstd writer: %w", err)
		}
		tw := tar.NewWriter(zw)
		return &tarArchiveWriter{
			tw:  tw,
			cw:  zw,
			fmt: format,
		}, nil

	case domain.CompZip:
		return nil, fmt.Errorf("zip 流式归档暂不支持，请使用 tar.gz 或 tar.zst")

	default:
		return nil, fmt.Errorf("不支持的归档格式: %s", format)
	}
}

// tarArchiveWriter tar 归档写入器（支持 tar.gz / tar.zst）
type tarArchiveWriter struct {
	tw  *tar.Writer
	cw  io.Closer
	fmt domain.CompressionFormat
}

// AddFile 写入一个文件条目
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

// Close 收尾，关闭 tar + 压缩 writer
func (w *tarArchiveWriter) Close() error {
	if err := w.tw.Close(); err != nil {
		return fmt.Errorf("关闭 tar writer: %w", err)
	}
	if err := w.cw.Close(); err != nil {
		return fmt.Errorf("关闭压缩 writer: %w", err)
	}
	return nil
}
