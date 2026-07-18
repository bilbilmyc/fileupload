package domain

import (
	"context"
	"io"
)

// Compressor 压缩/解压/打包抽象
type Compressor interface {
	// Decompress 输入压缩流，输出原始数据流
	Decompress(ctx context.Context, r io.Reader, format CompressionFormat) (io.ReadCloser, error)

	// NewArchiveWriter 返回一个可流式写入的归档器
	NewArchiveWriter(ctx context.Context, w io.Writer, format CompressionFormat) (ArchiveWriter, error)
}

// ArchiveWriter 流式归档写入器
type ArchiveWriter interface {
	// AddFile 写入一个文件条目
	AddFile(ctx context.Context, name string, size int64, content io.Reader) error
	// Close 收尾（写 footer / flush），完成后 w 可 Flush 到 HTTP
	Close() error
}
