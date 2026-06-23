package transport

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

// ShareStore 分享存储接口
type ShareStore interface {
	CreateShare(ctx context.Context, token string, entry *domain.ShareEntry) error
	GetShare(ctx context.Context, token string) (*domain.ShareEntry, error)
	IncrDownloads(ctx context.Context, token string) error
}

// ShareHandler 分享链接 HTTP 处理器
type ShareHandler struct {
	store      ShareStore
	downloadSvc *domain.DownloadService
}

// NewShareHandler 创建分享处理器
func NewShareHandler(store ShareStore, downloadSvc *domain.DownloadService) *ShareHandler {
	return &ShareHandler{store: store, downloadSvc: downloadSvc}
}

// CreateShare POST /v1/share
func (h *ShareHandler) CreateShare(w http.ResponseWriter, r *http.Request) {
	var req domain.CreateShareRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.FileID == "" {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}

	token := generateShareToken()
	entry := &domain.ShareEntry{
		Token:    token,
		FileID:   req.FileID,
		Namespace: GetNamespace(r.Context()),
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

	if err := h.store.CreateShare(r.Context(), token, entry); err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}

	respondJSON(w, http.StatusCreated, entry)
}

// AccessShare GET /s/{token}
func (h *ShareHandler) AccessShare(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.URL.Path, "/s/")
	if token == "" {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}

	entry, err := h.store.GetShare(r.Context(), token)
	if err != nil || entry == nil {
		respondError(w, http.StatusNotFound, domain.ErrNotFound)
		return
	}

	// 检查过期
	if entry.ExpiresAt != "" {
		exp, err := time.Parse(time.RFC3339, entry.ExpiresAt)
		if err == nil && time.Now().After(exp) {
			respondError(w, http.StatusGone, domain.ErrNotFound)
			return
		}
	}

	// 检查下载次数
	if entry.MaxDownloads > 0 && entry.CurDownloads >= entry.MaxDownloads {
		respondError(w, http.StatusGone, domain.ErrNotFound)
		return
	}

	// 验证密码（请求头 X-Share-Password）
	password := r.Header.Get("X-Share-Password")
	if entry.PasswordHash != "" {
		h := sha256.Sum256([]byte(password))
		if hex.EncodeToString(h[:]) != entry.PasswordHash {
			respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "密码错误", "code": "share_password_required"})
			return
		}
	}

	// 增加下载计数
	h.store.IncrDownloads(r.Context(), token)

	// 重定向到文件下载
	http.Redirect(w, r, "/v1/files/"+entry.FileID+"?namespace="+entry.Namespace, http.StatusFound)
}

func generateShareToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return "s-" + hex.EncodeToString(b)
}
