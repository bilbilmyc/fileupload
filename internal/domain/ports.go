package domain

import (
	"context"
	"io"
	"time"
)

// ============================================================
// 端口（Port）接口定义
// 领域核心通过这些接口与适配层交互，不依赖具体实现。
// ============================================================

// ---------- Storage 端口 ----------

// Storage 存储后端抽象（本地 FS / S3）
type Storage interface {
	// Write 从 reader 流式写到 path，返回写入字节数
	Write(ctx context.Context, path string, r io.Reader) (n int64, err error)

	// Open 打开读取句柄，支持 Range（length=0 表示到文件尾）
	Open(ctx context.Context, path string, offset, length int64) (io.ReadCloser, error)

	// Delete 删除物理文件
	Delete(ctx context.Context, path string) error

	// Stat 获取文件信息（大小、是否存在）
	Stat(ctx context.Context, path string) (size int64, exists bool, err error)
}

// ---------- Metadata 端口 ----------

// Metadata 元数据存储抽象（Redis 热数据 + DB 冷数据）
type Metadata interface {
	// === 热数据：上传会话（Redis） ===
	CreateSession(ctx context.Context, s *UploadSession) error
	GetSession(ctx context.Context, id string) (*UploadSession, error)
	UpdateOffset(ctx context.Context, id string, sliceIndex int, sliceSha string, addBytes int64) error
	ListChunks(ctx context.Context, id string) ([]ChunkInfo, error)
	DeleteSession(ctx context.Context, id string) error
	TouchSession(ctx context.Context, id string, ttl time.Duration) error
	ListExpiredSessions(ctx context.Context) ([]string, error) // reaper 用

	// === 冷数据：已完成文件（DB） ===
	// 内容寻址去重
	GetBlobBySha(ctx context.Context, sha256 string) (*ContentBlob, error)
	PutBlob(ctx context.Context, b *ContentBlob) error
	IncrBlobRef(ctx context.Context, sha256 string) error
	DecrBlobRef(ctx context.Context, sha256 string) (newCount int, err error)

	// 文件节点（逻辑文件 + 目录树）
	PutFile(ctx context.Context, f *FileMetadata) error
	GetFile(ctx context.Context, id string) (*FileMetadata, error)
	GetFileByPath(ctx context.Context, namespace, path string) (*FileMetadata, error)
	ListChildren(ctx context.Context, parentID string) ([]*FileMetadata, error)
	DeleteFile(ctx context.Context, id string) error
	ListFilesByBlob(ctx context.Context, sha256 string) ([]*FileMetadata, error) // 引用统计

	// 列目录（根目录 children 为空）
	ListRoot(ctx context.Context, namespace string) ([]*FileMetadata, error)

	// 一致性巡检
	ListAllBlobs(ctx context.Context) ([]*ContentBlob, error)
	ListAllFiles(ctx context.Context) ([]*FileMetadata, error)
}

// ---------- Compressor 端口 ----------

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

// ---------- Hasher 端口 ----------

// Hasher SHA-256 哈希抽象
type Hasher interface {
	// Sum 边读边算，返回 hex 哈希 + 读取字节数；完成后 reader 已耗尽
	Sum(ctx context.Context, r io.Reader) (sha256hex string, n int64, err error)

	// TeeReader 返回一个 reader，读它的同时累计哈希，最后通过 hash 方法取值
	TeeReader(r io.Reader) (io.Reader, HashAccumulator)
}

// HashAccumulator 累计哈希值接口，配合 TeeReader 使用
type HashAccumulator interface {
	// SumHex 返回当前已读取数据的哈希 hex
	SumHex() string
	// N 返回已读取字节数
	N() int64
}
