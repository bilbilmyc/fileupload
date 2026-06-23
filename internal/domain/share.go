package domain

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// ShareEntry 分享链接
type ShareEntry struct {
	Token         string `json:"token"`
	FileID        string `json:"file_id"`
	PasswordHash  string `json:"-"`
	ExpiresAt     string `json:"expires_at,omitempty"`
	MaxDownloads  int    `json:"max_downloads"`
	CurDownloads  int    `json:"cur_downloads"`
	Namespace     string `json:"namespace"`
	CreatedAt     string `json:"created_at,omitempty"`
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
	IncrDownloads(ctx context.Context, token string) error
}

// ShareService 分享业务逻辑
type ShareService struct {
	store ShareStore
}

// NewShareService 创建分享服务
func NewShareService(store ShareStore) *ShareService {
	return &ShareService{store: store}
}

// CreateShare 创建分享链接
func (s *ShareService) CreateShare(ctx context.Context, req CreateShareRequest, namespace string) (*ShareEntry, error) {
	if req.FileID == "" {
		return nil, ErrInvalidArgument
	}

	token := generateShareToken()
	entry := &ShareEntry{
		Token:     token,
		FileID:    req.FileID,
		Namespace: namespace,
	}

	if req.Password != "" {
		h := sha256.Sum256([]byte(req.Password))
		entry.PasswordHash = hex.EncodeToString(h[:])
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
//   entry — 分享条目
//   err — 错误（ErrNotFound 表示不存在/已过期/已达上限，ErrForbidden 表示密码错误）
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

	// 验证密码
	if entry.PasswordHash != "" {
		h := sha256.Sum256([]byte(password))
		if hex.EncodeToString(h[:]) != entry.PasswordHash {
			return nil, ErrForbidden
		}
	}

	// 增加下载计数
	_ = s.store.IncrDownloads(ctx, token)

	return entry, nil
}

// generateShareToken 生成分享 token
func generateShareToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "s-" + hex.EncodeToString(b)
}
