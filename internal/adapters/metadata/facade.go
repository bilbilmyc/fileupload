// Package metadata 实现 domain.Metadata 端口
// Redis 存热数据（会话），SQLite 存冷数据（blob + file）
package metadata

import (
	"context"
	"fmt"
	"time"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

// Facade Metadata 门面，路由请求到 RedisStore（热）或 SQLiteStore（冷）
type Facade struct {
	hot  *RedisStore  // 热数据：会话/分片/offset
	cold *SQLiteStore // 冷数据：content_blobs / files
}

// NewFacade 创建 Metadata 门面
func NewFacade(hot *RedisStore, cold *SQLiteStore) *Facade {
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

// ========== 冷数据：blob + file ==========

func (f *Facade) GetBlobBySha(ctx context.Context, sha256 string) (*domain.ContentBlob, error) {
	return f.cold.GetBlobBySha(ctx, sha256)
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

func (f *Facade) GetFileByPath(ctx context.Context, namespace, path string) (*domain.FileMetadata, error) {
	return f.cold.GetFileByPath(ctx, namespace, path)
}

func (f *Facade) ListChildren(ctx context.Context, parentID string) ([]*domain.FileMetadata, error) {
	return f.cold.ListChildren(ctx, parentID)
}

func (f *Facade) DeleteFile(ctx context.Context, id string) error {
	return f.cold.DeleteFile(ctx, id)
}

func (f *Facade) ListFilesByBlob(ctx context.Context, sha256 string) ([]*domain.FileMetadata, error) {
	return f.cold.ListFilesByBlob(ctx, sha256)
}

func (f *Facade) ListRoot(ctx context.Context, namespace string) ([]*domain.FileMetadata, error) {
	return f.cold.ListRoot(ctx, namespace)
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

// Close 关闭所有后端连接
func (f *Facade) Close() error {
	hotErr := f.hot.Close()
	coldErr := f.cold.Close()
	if hotErr != nil {
		return fmt.Errorf("关闭热存储: %w", hotErr)
	}
	return coldErr
}
