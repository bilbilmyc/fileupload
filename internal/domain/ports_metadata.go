package domain

import (
	"context"
	"time"
)

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

// SessionFinalizer 原子地将上传会话从 active 抢占为 finalizing。
// 具体实现可使用 Redis WATCH/Lua；未实现该可选接口的存储保留兼容路径。
type SessionFinalizer interface {
	ClaimSessionFinalizing(ctx context.Context, id string) (*UploadSession, error)
}

// BlobCommitter 原子获取内容 blob 的引用。若内容已存在，实现必须递增引用并返回已有存储路径。
type BlobCommitter interface {
	AcquireBlob(ctx context.Context, b *ContentBlob) (storagePath string, inserted bool, err error)
}

// NamespaceQuotaReservoir atomically reserves logical namespace capacity.
// Reservations are keyed by an upload/operation ID and must be released when
// the operation finishes or is aborted.
type NamespaceQuotaReservoir interface {
	ReserveNamespaceBytes(ctx context.Context, namespace, reservationID string, bytes, quota int64) error
	ReleaseNamespaceReservation(ctx context.Context, reservationID string) error
}

// BlobStore 内容寻址去重数据接口
// BatchBlobStore exposes an optional bulk lookup used by batch downloads.
// Implementations that do not provide it remain compatible; callers fall back to
// the single-item BlobStore methods.
type BatchBlobStore interface {
	GetBlobsBySha(ctx context.Context, sha256s []string) (map[string]*ContentBlob, error)
}

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
	ListChildrenPage(ctx context.Context, parentID string, search, fileType string, page, perPage int, sortBy, sortOrder string) ([]*FileMetadata, int, error)
	ListRoot(ctx context.Context, namespace string, search string) ([]*FileMetadata, error)
	ListRootPage(ctx context.Context, namespace string, search, fileType string, page, perPage int, sortBy, sortOrder string) ([]*FileMetadata, int, error)
	GetNamespaceUsage(ctx context.Context, namespace string) (*NamespaceUsage, error)
	ListTrash(ctx context.Context, namespace string) ([]*FileMetadata, error)
	MoveFileToTrash(ctx context.Context, id string, deletedAt time.Time) error
	RestoreFile(ctx context.Context, id string) error
	DeleteFile(ctx context.Context, id string) error
	ListFilesByBlob(ctx context.Context, sha256 string) ([]*FileMetadata, error)
	ReparentFile(ctx context.Context, fileID string, parentID *string, path string) error
	UpdateFileParent(ctx context.Context, fileID string, parentID *string) error
	RenameFile(ctx context.Context, fileID, newName, newPath string) error
	SetFileTags(ctx context.Context, fileID string, tags []string) error
	GetFileTags(ctx context.Context, fileID string) ([]string, error)
	DeleteFileTags(ctx context.Context, fileID string) error
}

// BatchFileStore exposes an optional bulk lookup used by batch downloads.
// Implementations that do not provide it remain compatible; callers fall back to
// the single-item FileStore methods.
type BatchFileStore interface {
	GetFilesByIDs(ctx context.Context, ids []string) ([]*FileMetadata, error)
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
	ShareStore

	// HealthCheck 健康检查：返回 nil 表示后端可用。
	HealthCheck(ctx context.Context) error
}
