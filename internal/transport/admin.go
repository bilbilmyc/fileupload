package transport

import (
	"context"
	"net/http"
	"strconv"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

// AdminHandler 管理后台 HTTP 处理器
type AdminHandler struct {
	meta       interface {
		WriteAuditLog(ctx context.Context, entry *domain.AuditLogEntry) error
		ListAuditLogs(ctx context.Context, page, perPage int) ([]*domain.AuditLogEntry, int, error)
		AdminCountFiles(ctx context.Context) (int, error)
		AdminCountBlobs(ctx context.Context) (int, error)
		AdminTotalBlobSize(ctx context.Context) (int64, error)
	}
	workerPool interface {
		Stats() domain.WorkerStats
	}
	cfg struct {
		DataDir string
		TempDir string
		DBPath  string
		DBType  string
	}
}

// NewAdminHandler 创建管理后台处理器
func NewAdminHandler(meta interface {
	WriteAuditLog(ctx context.Context, entry *domain.AuditLogEntry) error
	ListAuditLogs(ctx context.Context, page, perPage int) ([]*domain.AuditLogEntry, int, error)
	AdminCountFiles(ctx context.Context) (int, error)
	AdminCountBlobs(ctx context.Context) (int, error)
	AdminTotalBlobSize(ctx context.Context) (int64, error)
}, workerPool interface {
	Stats() domain.WorkerStats
}, dataDir, tempDir, dbPath, dbType string) *AdminHandler {
	h := &AdminHandler{
		meta:       meta,
		workerPool: workerPool,
	}
	h.cfg.DataDir = dataDir
	h.cfg.TempDir = tempDir
	h.cfg.DBPath = dbPath
	h.cfg.DBType = dbType
	return h
}

// Status GET /v1/admin/status
func (h *AdminHandler) Status(w http.ResponseWriter, r *http.Request) {
	fileCount, _ := h.meta.AdminCountFiles(r.Context())
	blobCount, _ := h.meta.AdminCountBlobs(r.Context())
	totalSize, _ := h.meta.AdminTotalBlobSize(r.Context())

	poolStats := h.workerPool.Stats()

	respondJSON(w, http.StatusOK, map[string]any{
		"version": "dev",
		"storage": map[string]any{
			"data_dir":    h.cfg.DataDir,
			"temp_dir":    h.cfg.TempDir,
			"total_files": fileCount,
			"total_blobs": blobCount,
			"total_size":  totalSize,
		},
		"database": map[string]any{
			"type": h.cfg.DBType,
			"path": h.cfg.DBPath,
		},
		"worker_pool": map[string]any{
			"capacity":  poolStats.Capacity,
			"available": poolStats.Available,
		},
	})
}

// AuditLog GET /v1/admin/audit
func (h *AdminHandler) AuditLog(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if perPage < 1 || perPage > 100 {
		perPage = 50
	}

	entries, total, err := h.meta.ListAuditLogs(r.Context(), page, perPage)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	if entries == nil {
		entries = []*domain.AuditLogEntry{}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"entries":  entries,
		"total":    total,
		"page":     page,
		"per_page": perPage,
	})
}
