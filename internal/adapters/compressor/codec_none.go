package compressor

import (
	"context"
	"io"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

// noneCodec 原始数据透传（不压缩不解压）。
type noneCodec struct{}

func (noneCodec) Format() domain.CompressionFormat { return domain.CompNone }

func (noneCodec) Decompress(_ context.Context, r io.Reader) (io.ReadCloser, error) {
	return io.NopCloser(r), nil
}

func init() { RegisterDecompressor(noneCodec{}) }
