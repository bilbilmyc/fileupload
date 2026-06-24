// Package compressor 实现 domain.Compressor 端口的压缩/解压/归档适配器。
//
// 采用 codec 注册表模式：每种格式（zstd / gzip / tar.gz / tar.zst / zip / none）
// 在独立文件中以 Decompressor / Archiver 接口实现，并在 init() 注册到包级注册表。
// 加新格式 = 新增一个 codec_xxx.go 文件 + init() 注册一行，不再修改调度文件。
package compressor

import (
	"context"
	"fmt"
	"io"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

// Decompressor 单流解压：把压缩流还原为原始字节流。
// 例如 gzip / zstd / tar.gz / tar.zst 都属于此类（gzip/tar.* 仅剥离外层压缩，不解 tar）。
type Decompressor interface {
	Format() domain.CompressionFormat
	Decompress(ctx context.Context, r io.Reader) (io.ReadCloser, error)
}

// Archiver 归档写入：把多个文件打包成单一归档流（tar.gz / tar.zst / zip）。
type Archiver interface {
	Format() domain.CompressionFormat
	NewArchiveWriter(ctx context.Context, w io.Writer) (domain.ArchiveWriter, error)
}

// 注册表：Decompressor 与 Archiver 分别注册，允许同格式仅支持一种操作。
var (
	decompressors = map[domain.CompressionFormat]Decompressor{}
	archivers     = map[domain.CompressionFormat]Archiver{}
)

// RegisterDecompressor 注册单流解压 codec。
func RegisterDecompressor(d Decompressor) { decompressors[d.Format()] = d }

// RegisterArchiver 注册归档写入 codec。
func RegisterArchiver(a Archiver) { archivers[a.Format()] = a }

// Compressor 压缩/解压/归档调度器。
// 自身不持有 codec 状态 — 调度完全走包级注册表。
// 这种结构消除了之前 Compressor 上的 zstdDec/zstdEnc 字段（init 后从未使用）。
type Compressor struct{}

// NewCompressor 创建 Compressor。注册表中的 codec 在其各自的 init() 中已注册。
func NewCompressor() (*Compressor, error) {
	if len(decompressors) == 0 && len(archivers) == 0 {
		return nil, fmt.Errorf("compressor: 没有任何 codec 注册")
	}
	return &Compressor{}, nil
}

// Decompress 解压：输入压缩流，输出原始数据流。
func (c *Compressor) Decompress(ctx context.Context, r io.Reader, format domain.CompressionFormat) (io.ReadCloser, error) {
	d, ok := decompressors[format]
	if !ok {
		return nil, fmt.Errorf("不支持的压缩格式: %s", format)
	}
	return d.Decompress(ctx, r)
}

// NewArchiveWriter 创建流式归档写入器。
func (c *Compressor) NewArchiveWriter(ctx context.Context, w io.Writer, format domain.CompressionFormat) (domain.ArchiveWriter, error) {
	a, ok := archivers[format]
	if !ok {
		return nil, fmt.Errorf("不支持的归档格式: %s", format)
	}
	return a.NewArchiveWriter(ctx, w)
}