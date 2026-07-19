// Package metadata 实现 domain.Metadata 端口
// Redis 存热数据（会话），SQLite 存冷数据（blob + file）
package metadata

import (
	"context"
	"fmt"
	"time"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

// ColdStore 冷数据存储接口（SQLite / PostgreSQL 均可实现）
type ColdStore interface {
	// Blob 操作
	GetBlobBySha(ctx context.Context, sha256 string) (*domain.ContentBlob, error)
	GetBlobsBySha(ctx context.Context, sha256s []string) (map[string]*domain.ContentBlob, error)
	PutBlob(ctx context.Context, b *domain.ContentBlob) error
	UpdateBlobStorage(ctx context.Context, sha256 string, storagePath string) error
	IncrBlobRef(ctx context.Context, sha256 string) error
	DecrBlobRef(ctx context.Context, sha256 string) (int, error)

	// 文件操作
	PutFile(ctx context.Context, f *domain.FileMetadata) error
	GetFile(ctx context.Context, id string) (*domain.FileMetadata, error)
	GetFilesByIDs(ctx context.Context, ids []string) ([]*domain.FileMetadata, error)
	GetFileByPath(ctx context.Context, namespace, path string) (*domain.FileMetadata, error)
	ListChildren(ctx context.Context, parentID string, search string) ([]*domain.FileMetadata, error)
	ListChildrenPage(ctx context.Context, parentID string, search, fileType string, page, perPage int, sortBy, sortOrder string) ([]*domain.FileMetadata, int, error)
	DeleteFile(ctx context.Context, id string) error
	ListFilesByBlob(ctx context.Context, sha256 string) ([]*domain.FileMetadata, error)
	ListRoot(ctx context.Context, namespace string, search string) ([]*domain.FileMetadata, error)
	ListRootPage(ctx context.Context, namespace string, search, fileType string, page, perPage int, sortBy, sortOrder string) ([]*domain.FileMetadata, int, error)
	GetNamespaceUsage(ctx context.Context, namespace string) (*domain.NamespaceUsage, error)
	ListTrash(ctx context.Context, namespace string) ([]*domain.FileMetadata, error)
	MoveFileToTrash(ctx context.Context, id string, deletedAt time.Time) error
	RestoreFile(ctx context.Context, id string) error

	// 标签
	SetFileTags(ctx context.Context, fileID string, tags []string) error
	GetFileTags(ctx context.Context, fileID string) ([]string, error)
	DeleteFileTags(ctx context.Context, fileID string) error

	// 批量操作
	ReparentFile(ctx context.Context, fileID string, parentID *string, path string) error
	UpdateFileParent(ctx context.Context, fileID string, parentID *string) error

	// 一致性巡检
	ListAllBlobs(ctx context.Context) ([]*domain.ContentBlob, error)
	ListAllFiles(ctx context.Context) ([]*domain.FileMetadata, error)

	// 分享
	CreateShare(ctx context.Context, token string, entry *domain.ShareEntry) error
	GetShare(ctx context.Context, token string) (*domain.ShareEntry, error)
	ListShares(ctx context.Context, namespace, fileID string) ([]*domain.ShareEntry, error)
	DeleteShare(ctx context.Context, token, namespace string) error
	IncrDownloads(ctx context.Context, token string) error

	// 审计日志
	WriteAuditLog(ctx context.Context, entry *domain.AuditLogEntry) error
	ListAuditLogs(ctx context.Context, page, perPage int) ([]*domain.AuditLogEntry, int, error)
	AdminCountFiles(ctx context.Context) (int, error)
	AdminCountBlobs(ctx context.Context) (int, error)
	AdminTotalBlobSize(ctx context.Context) (int64, error)

	RenameFile(ctx context.Context, fileID, newName, newPath string) error
	Close() error
	HealthCheck(ctx context.Context) error
}

// HotStore 热数据存储接口（Redis）。扩展 domain.SessionStore 加上 Close()。
type HotStore interface {
	domain.SessionStore
	Close() error
	HealthCheck(ctx context.Context) error
}

// Facade Metadata 门面，路由请求到 HotStore（热）或 ColdStore（冷）
type Facade struct {
	hot  HotStore  // 热数据：会话/分片/offset
	cold ColdStore // 冷数据：content_blobs / files
}

// NewFacade 创建 Metadata 门面
func NewFacade(hot HotStore, cold ColdStore) *Facade {
	return &Facade{hot: hot, cold: cold}
}

// ========== 热数据：会话 ==========

func (f *Facade) CreateSession(ctx context.Context, s *domain.UploadSession) error {
	return f.hot.CreateSession(ctx, s)
}

func (f *Facade) GetSession(ctx context.Context, id string) (*domain.UploadSession, error) {
	return f.hot.GetSession(ctx, id)
}

func (f *Facade) UpdateOffset(ctx context.Context, id string, sliceIndex int, sliceSha string, addBytes int64) error {
	return f.hot.UpdateOffset(ctx, id, sliceIndex, sliceSha, addBytes)
}

func (f *Facade) ListChunks(ctx context.Context, id string) ([]domain.ChunkInfo, error) {
	return f.hot.ListChunks(ctx, id)
}

func (f *Facade) DeleteSession(ctx context.Context, id string) error {
	return f.hot.DeleteSession(ctx, id)
}

func (f *Facade) TouchSession(ctx context.Context, id string, ttl time.Duration) error {
	return f.hot.TouchSession(ctx, id, ttl)
}

func (f *Facade) ListExpiredSessions(ctx context.Context) ([]string, error) {
	return f.hot.ListExpiredSessions(ctx)
}

// ClaimSessionFinalizing 将可选的原子 finalize 抢占能力透传给热存储。
func (f *Facade) ClaimSessionFinalizing(ctx context.Context, id string) (*domain.UploadSession, error) {
	if finalizer, ok := f.hot.(domain.SessionFinalizer); ok {
		return finalizer.ClaimSessionFinalizing(ctx, id)
	}
	s, err := f.hot.GetSession(ctx, id)
	if err != nil {
		return nil, err
	}
	if s == nil {
		return nil, domain.ErrSessionNotFound
	}
	if s.Status != domain.SessionActive {
		return nil, domain.ErrSessionState
	}
	s.Status = domain.SessionFinalizing
	return s, nil
}

// ========== 冷数据：blob + file ==========

func (f *Facade) GetBlobBySha(ctx context.Context, sha256 string) (*domain.ContentBlob, error) {
	return f.cold.GetBlobBySha(ctx, sha256)
}

func (f *Facade) GetBlobsBySha(ctx context.Context, sha256s []string) (map[string]*domain.ContentBlob, error) {
	return f.cold.GetBlobsBySha(ctx, sha256s)
}

func (f *Facade) PutBlob(ctx context.Context, b *domain.ContentBlob) error {
	return f.cold.PutBlob(ctx, b)
}

func (f *Facade) IncrBlobRef(ctx context.Context, sha256 string) error {
	return f.cold.IncrBlobRef(ctx, sha256)
}

func (f *Facade) DecrBlobRef(ctx context.Context, sha256 string) (int, error) {
	return f.cold.DecrBlobRef(ctx, sha256)
}

func (f *Facade) PutFile(ctx context.Context, file *domain.FileMetadata) error {
	return f.cold.PutFile(ctx, file)
}

func (f *Facade) GetFile(ctx context.Context, id string) (*domain.FileMetadata, error) {
	return f.cold.GetFile(ctx, id)
}

func (f *Facade) GetFilesByIDs(ctx context.Context, ids []string) ([]*domain.FileMetadata, error) {
	return f.cold.GetFilesByIDs(ctx, ids)
}

func (f *Facade) GetFileByPath(ctx context.Context, namespace, path string) (*domain.FileMetadata, error) {
	return f.cold.GetFileByPath(ctx, namespace, path)
}

func (f *Facade) ListChildrenPage(ctx context.Context, parentID string, search, fileType string, page, perPage int, sortBy, sortOrder string) ([]*domain.FileMetadata, int, error) {
	return f.cold.ListChildrenPage(ctx, parentID, search, fileType, page, perPage, sortBy, sortOrder)
}

func (f *Facade) ListChildren(ctx context.Context, parentID string, search string) ([]*domain.FileMetadata, error) {
	return f.cold.ListChildren(ctx, parentID, search)
}

func (f *Facade) DeleteFile(ctx context.Context, id string) error {
	return f.cold.DeleteFile(ctx, id)
}

func (f *Facade) ListFilesByBlob(ctx context.Context, sha256 string) ([]*domain.FileMetadata, error) {
	return f.cold.ListFilesByBlob(ctx, sha256)
}

func (f *Facade) ListRootPage(ctx context.Context, namespace string, search, fileType string, page, perPage int, sortBy, sortOrder string) ([]*domain.FileMetadata, int, error) {
	return f.cold.ListRootPage(ctx, namespace, search, fileType, page, perPage, sortBy, sortOrder)
}

func (f *Facade) ListRoot(ctx context.Context, namespace string, search string) ([]*domain.FileMetadata, error) {
	return f.cold.ListRoot(ctx, namespace, search)
}

func (f *Facade) GetNamespaceUsage(ctx context.Context, namespace string) (*domain.NamespaceUsage, error) {
	return f.cold.GetNamespaceUsage(ctx, namespace)
}

func (f *Facade) ListTrash(ctx context.Context, namespace string) ([]*domain.FileMetadata, error) {
	return f.cold.ListTrash(ctx, namespace)
}

func (f *Facade) MoveFileToTrash(ctx context.Context, id string, deletedAt time.Time) error {
	return f.cold.MoveFileToTrash(ctx, id, deletedAt)
}

func (f *Facade) RestoreFile(ctx context.Context, id string) error {
	return f.cold.RestoreFile(ctx, id)
}

func (f *Facade) UpdateBlobStorage(ctx context.Context, sha256 string, storagePath string) error {
	return f.cold.UpdateBlobStorage(ctx, sha256, storagePath)
}

// AcquireBlob 将可选的原子 blob 获取能力透传给冷存储。
// ReserveNamespaceBytes 将可选的原子配额预留能力透传给冷存储。
func (f *Facade) ReserveNamespaceBytes(ctx context.Context, namespace, reservationID string, bytes, quota int64) error {
	if reservoir, ok := f.cold.(domain.NamespaceQuotaReservoir); ok {
		return reservoir.ReserveNamespaceBytes(ctx, namespace, reservationID, bytes, quota)
	}
	return nil
}

func (f *Facade) ReleaseNamespaceReservation(ctx context.Context, reservationID string) error {
	if reservoir, ok := f.cold.(domain.NamespaceQuotaReservoir); ok {
		return reservoir.ReleaseNamespaceReservation(ctx, reservationID)
	}
	return nil
}

func (f *Facade) AcquireBlob(ctx context.Context, b *domain.ContentBlob) (string, bool, error) {
	if committer, ok := f.cold.(domain.BlobCommitter); ok {
		return committer.AcquireBlob(ctx, b)
	}
	if err := f.cold.PutBlob(ctx, b); err != nil {
		return "", false, err
	}
	return b.StoragePath, true, nil
}

func (f *Facade) RenameFile(ctx context.Context, fileID, newName, newPath string) error {
	return f.cold.RenameFile(ctx, fileID, newName, newPath)
}

func (f *Facade) SetFileTags(ctx context.Context, fileID string, tags []string) error {
	return f.cold.SetFileTags(ctx, fileID, tags)
}

func (f *Facade) GetFileTags(ctx context.Context, fileID string) ([]string, error) {
	return f.cold.GetFileTags(ctx, fileID)
}

func (f *Facade) DeleteFileTags(ctx context.Context, fileID string) error {
	return f.cold.DeleteFileTags(ctx, fileID)
}

func (f *Facade) ReparentFile(ctx context.Context, fileID string, parentID *string, path string) error {
	return f.cold.ReparentFile(ctx, fileID, parentID, path)
}

func (f *Facade) UpdateFileParent(ctx context.Context, fileID string, parentID *string) error {
	return f.cold.UpdateFileParent(ctx, fileID, parentID)
}

func (f *Facade) ListAllBlobs(ctx context.Context) ([]*domain.ContentBlob, error) {
	return f.cold.ListAllBlobs(ctx)
}

func (f *Facade) ListAllFiles(ctx context.Context) ([]*domain.FileMetadata, error) {
	return f.cold.ListAllFiles(ctx)
}

// ========== 分享 ==========

func (f *Facade) CreateShare(ctx context.Context, token string, entry *domain.ShareEntry) error {
	return f.cold.CreateShare(ctx, token, entry)
}

func (f *Facade) GetShare(ctx context.Context, token string) (*domain.ShareEntry, error) {
	return f.cold.GetShare(ctx, token)
}

func (f *Facade) ListShares(ctx context.Context, namespace, fileID string) ([]*domain.ShareEntry, error) {
	return f.cold.ListShares(ctx, namespace, fileID)
}

func (f *Facade) DeleteShare(ctx context.Context, token, namespace string) error {
	return f.cold.DeleteShare(ctx, token, namespace)
}

func (f *Facade) IncrDownloads(ctx context.Context, token string) error {
	return f.cold.IncrDownloads(ctx, token)
}

// TryConsumeDownload 将可选的原子分享额度消耗能力透传给冷存储。
func (f *Facade) TryConsumeDownload(ctx context.Context, token string) (bool, error) {
	if consumer, ok := f.cold.(domain.ShareDownloadConsumer); ok {
		return consumer.TryConsumeDownload(ctx, token)
	}
	if err := f.cold.IncrDownloads(ctx, token); err != nil {
		return false, err
	}
	return true, nil
}

// ========== 管理后台 ==========

func (f *Facade) WriteAuditLog(ctx context.Context, entry *domain.AuditLogEntry) error {
	return f.cold.WriteAuditLog(ctx, entry)
}

func (f *Facade) ListAuditLogs(ctx context.Context, page, perPage int) ([]*domain.AuditLogEntry, int, error) {
	return f.cold.ListAuditLogs(ctx, page, perPage)
}

func (f *Facade) AdminCountFiles(ctx context.Context) (int, error) {
	return f.cold.AdminCountFiles(ctx)
}

func (f *Facade) AdminCountBlobs(ctx context.Context) (int, error) {
	return f.cold.AdminCountBlobs(ctx)
}

func (f *Facade) AdminTotalBlobSize(ctx context.Context) (int64, error) {
	return f.cold.AdminTotalBlobSize(ctx)
}

// Close 关闭所有后端连接
func (f *Facade) Close() error {
	hotErr := f.hot.Close()
	coldErr := f.cold.Close()
	if hotErr != nil {
		return fmt.Errorf("关闭热存储: %w", hotErr)
	}
	return coldErr
}

// HealthCheck 组合检查：先热后冷，任一失败立即返回。
func (f *Facade) HealthCheck(ctx context.Context) error {
	if err := f.hot.HealthCheck(ctx); err != nil {
		return fmt.Errorf("热存储: %w", err)
	}
	if err := f.cold.HealthCheck(ctx); err != nil {
		return fmt.Errorf("冷存储: %w", err)
	}
	return nil
}
