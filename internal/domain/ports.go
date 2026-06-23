package domain

import (
	"context"
	"io"
	"io/fs"
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

	// Walk 遍历存储，fn 收到的是相对于 root 的路径。
	// 用于一致性巡检、批量列举等场景。
	Walk(ctx context.Context, fn func(path string, info fs.FileInfo) error) error
}

// ---------- Metadata 端口（拆分后子接口） ----------

// SessionStore 上传会话热数据接口（Redis）
type SessionStore interface {
	CreateSession(ctx context.Context, s *UploadSession) error
	GetSession(ctx context.Context, id string) (*UploadSession, error)
	UpdateOffset(ctx context.Context, id string, sliceIndex int, sliceSha string, addBytes int64) error
	ListChunks(ctx context.Context, id string) ([]ChunkInfo, error)
	DeleteSession(ctx context.Context, id string) error
	TouchSession(ctx context.Context, id string, ttl time.Duration) error
	ListExpiredSessions(ctx context.Context) ([]string, error)
}

// BlobStore 内容寻址去重数据接口
type BlobStore interface {
	GetBlobBySha(ctx context.Context, sha256 string) (*ContentBlob, error)
	PutBlob(ctx context.Context, b *ContentBlob) error
	IncrBlobRef(ctx context.Context, sha256 string) error
	DecrBlobRef(ctx context.Context, sha256 string) (newCount int, err error)
	UpdateBlobStorage(ctx context.Context, sha256 string, storagePath string) error
}

// FileStore 文件和目录树接口
type FileStore interface {
	PutFile(ctx context.Context, f *FileMetadata) error
	GetFile(ctx context.Context, id string) (*FileMetadata, error)
	GetFileByPath(ctx context.Context, namespace, path string) (*FileMetadata, error)
	ListChildren(ctx context.Context, parentID string, search string) ([]*FileMetadata, error)
	ListRoot(ctx context.Context, namespace string, search string) ([]*FileMetadata, error)
	DeleteFile(ctx context.Context, id string) error
	ListFilesByBlob(ctx context.Context, sha256 string) ([]*FileMetadata, error)
	ReparentFile(ctx context.Context, fileID string, parentID *string, path string) error
	UpdateFileParent(ctx context.Context, fileID string, parentID *string) error
	SetFileTags(ctx context.Context, fileID string, tags []string) error
	GetFileTags(ctx context.Context, fileID string) ([]string, error)
	DeleteFileTags(ctx context.Context, fileID string) error
}

// AdminStore 管理后台接口（审计日志 + 计数 + 巡检）
type AdminStore interface {
	WriteAuditLog(ctx context.Context, entry *AuditLogEntry) error
	ListAuditLogs(ctx context.Context, page, perPage int) ([]*AuditLogEntry, int, error)
	AdminCountFiles(ctx context.Context) (int, error)
	AdminCountBlobs(ctx context.Context) (int, error)
	AdminTotalBlobSize(ctx context.Context) (int64, error)
	ListAllBlobs(ctx context.Context) ([]*ContentBlob, error)
	ListAllFiles(ctx context.Context) ([]*FileMetadata, error)
}

// Metadata 完整元数据接口（组合全部子接口，向前兼容）
type Metadata interface {
	SessionStore
	BlobStore
	FileStore
	AdminStore
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
