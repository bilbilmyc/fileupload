package domain

import "context"

// ============================================================
// 管理后台领域模型
// ============================================================

// AdminService 管理后台接口
type AdminService interface {
	// GetStatus 获取系统状态
	GetStatus(ctx context.Context) (*SystemStatus, error)
	// GetAuditLogs 获取审计日志（分页）
	GetAuditLogs(ctx context.Context, page, perPage int) (*AuditLogPage, error)
	// WriteAuditLog 写入审计日志
	WriteAuditLog(ctx context.Context, entry *AuditLogEntry) error
}

// SystemStatus 系统状态
type SystemStatus struct {
	Uptime        string              `json:"uptime"`
	Version       string              `json:"version"`
	WorkerPool    *WorkerPoolStatus   `json:"worker_pool"`
	Storage       *StorageStatus      `json:"storage"`
	Database      *DatabaseStatus     `json:"database"`
}

// WorkerPoolStatus worker 池状态
type WorkerPoolStatus struct {
	Capacity  int `json:"capacity"`
	Available int `json:"available"`
	Queued    int `json:"queued"`
}

// StorageStatus 存储状态
type StorageStatus struct {
	DataDir        string `json:"data_dir"`
	DataDirFree    string `json:"data_dir_free"`
	TempDir        string `json:"temp_dir"`
	TempDirFree    string `json:"temp_dir_free"`
	TotalFiles     int    `json:"total_files"`
	TotalBlobs     int    `json:"total_blobs"`
	TotalSize      string `json:"total_size"`
}

// DatabaseStatus 数据库状态
type DatabaseStatus struct {
	Type    string `json:"type"`
	Path    string `json:"path"`
	Status  string `json:"status"`
}

// AuditLogEntry 审计日志条目
type AuditLogEntry struct {
	ID         int64  `json:"id,omitempty"`
	Action     string `json:"action"`     // login, delete, batch_delete, batch_move, etc.
	TargetType string `json:"target_type"` // file, dir, session, user
	TargetID   string `json:"target_id"`
	UserID     string `json:"user_id"`
	Namespace  string `json:"namespace"`
	Detail     string `json:"detail"`
	CreatedAt  string `json:"created_at,omitempty"`
}

// AuditLogPage 审计日志分页结果
type AuditLogPage struct {
	Entries []*AuditLogEntry `json:"entries"`
	Total   int              `json:"total"`
	Page    int              `json:"page"`
	PerPage int              `json:"per_page"`
}
