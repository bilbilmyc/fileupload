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
	ListChildrenPage(ctx context.Context, parentID string, search string, page, perPage int, sortBy, sortOrder string) ([]*FileMetadata, int, error)
	ListRoot(ctx context.Context, namespace string, search string) ([]*FileMetadata, error)
	ListRootPage(ctx context.Context, namespace string, search string, page, perPage int, sortBy, sortOrder string) ([]*FileMetadata, int, error)
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
