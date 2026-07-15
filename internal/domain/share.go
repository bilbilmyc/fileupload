package domain

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// ShareEntry 分享链接
type ShareEntry struct {
	Token             string `json:"token"`
	FileID            string `json:"file_id"`
	PasswordHash      string `json:"-"`
	PasswordProtected bool   `json:"password_protected"`
	ExpiresAt         string `json:"expires_at,omitempty"`
	MaxDownloads      int    `json:"max_downloads"`
	CurDownloads      int    `json:"cur_downloads"`
	Namespace         string `json:"namespace"`
	CreatedAt         string `json:"created_at,omitempty"`
}

// CreateShareRequest 创建分享请求
type CreateShareRequest struct {
	FileID       string `json:"file_id"`
	Password     string `json:"password,omitempty"`
	ExpiresIn    int    `json:"expires_in"`    // 过期小时数，0=不限
	MaxDownloads int    `json:"max_downloads"` // 0=不限
}

// ShareStore 分享存储接口
type ShareStore interface {
	CreateShare(ctx context.Context, token string, entry *ShareEntry) error
	GetShare(ctx context.Context, token string) (*ShareEntry, error)
	ListShares(ctx context.Context, namespace, fileID string) ([]*ShareEntry, error)
	DeleteShare(ctx context.Context, token, namespace string) error
	IncrDownloads(ctx context.Context, token string) error
}

// ShareService 分享业务逻辑
type ShareService struct {
	store ShareStore
	files FileStore
}

// NewShareService 创建分享服务
func NewShareService(store ShareStore) *ShareService {
	service := &ShareService{store: store}
	if files, ok := store.(FileStore); ok {
		service.files = files
	}
	return service
}

// CreateShare 创建分享链接
func (s *ShareService) CreateShare(ctx context.Context, req CreateShareRequest, namespace string) (*ShareEntry, error) {
	if req.FileID == "" {
		return nil, ErrInvalidArgument
	}
	if s.files != nil {
		file, err := s.files.GetFile(ctx, req.FileID)
		if err != nil {
			return nil, fmt.Errorf("获取分享文件: %w", err)
		}
		if file == nil {
			return nil, ErrNotFound
		}
		if file.Namespace != namespace {
			return nil, ErrForbidden
		}
		if file.IsDir {
			return nil, ErrInvalidArgument
		}
	}

	token := generateShareToken()
	entry := &ShareEntry{
		Token:     token,
		FileID:    req.FileID,
		Namespace: namespace,
	}

	if req.Password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			return nil, fmt.Errorf("密码哈希失败: %w", err)
		}
		entry.PasswordHash = string(hash)
		entry.PasswordProtected = true
	}
	if req.ExpiresIn > 0 {
		entry.ExpiresAt = time.Now().Add(time.Duration(req.ExpiresIn) * time.Hour).Format(time.RFC3339)
	}
	if req.MaxDownloads > 0 {
		entry.MaxDownloads = req.MaxDownloads
	}

	if err := s.store.CreateShare(ctx, token, entry); err != nil {
		return nil, fmt.Errorf("创建分享记录: %w", err)
	}
	return entry, nil
}

// AccessShare 访问分享链接
// 返回值含义：
//
//	entry — 分享条目
//	err — 错误（ErrNotFound 表示不存在/已过期/已达上限，ErrForbidden 表示密码错误）
func (s *ShareService) AccessShare(ctx context.Context, token, password string) (*ShareEntry, error) {
	entry, err := s.store.GetShare(ctx, token)
	if err != nil || entry == nil {
		return nil, ErrNotFound
	}

	// 检查过期
	if entry.ExpiresAt != "" {
		exp, err := time.Parse(time.RFC3339, entry.ExpiresAt)
		if err == nil && time.Now().After(exp) {
			return nil, ErrShareExhausted
		}
	}

	// 检查下载次数
	if entry.MaxDownloads > 0 && entry.CurDownloads >= entry.MaxDownloads {
		return nil, ErrShareExhausted
	}

	// 验证密码（bcrypt）
	entry.PasswordProtected = entry.PasswordHash != ""
	if entry.PasswordHash != "" {
		if err := bcrypt.CompareHashAndPassword([]byte(entry.PasswordHash), []byte(password)); err != nil {
			return nil, ErrForbidden
		}
	}

	// 增加下载计数
	_ = s.store.IncrDownloads(ctx, token)

	return entry, nil
}

// ListShares 返回当前空间下的分享链接；fileID 为空时返回整个空间。
func (s *ShareService) ListShares(ctx context.Context, namespace, fileID string) ([]*ShareEntry, error) {
	entries, err := s.store.ListShares(ctx, namespace, fileID)
	if err != nil {
		return nil, fmt.Errorf("列举分享链接: %w", err)
	}
	for _, entry := range entries {
		entry.PasswordProtected = entry.PasswordHash != ""
	}
	return entries, nil
}

// RevokeShare 撤销当前空间中的分享链接。撤销后 token 立即失效。
func (s *ShareService) RevokeShare(ctx context.Context, token, namespace string) error {
	if token == "" {
		return ErrInvalidArgument
	}
	if err := s.store.DeleteShare(ctx, token, namespace); err != nil {
		return fmt.Errorf("撤销分享链接: %w", err)
	}
	return nil
}

// generateShareToken 生成分享 token
func generateShareToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "s-" + hex.EncodeToString(b)
}
