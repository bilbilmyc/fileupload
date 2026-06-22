package metadata

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

// AddAuditLogMigration 向 SQLiteStore 添加 audit_log 表迁移
// 在 migrate() 中的最后一个查询后执行
const auditLogMigration = `CREATE TABLE IF NOT EXISTS audit_log (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	action     TEXT NOT NULL,
	target_type TEXT NOT NULL DEFAULT '',
	target_id  TEXT NOT NULL DEFAULT '',
	user_id    TEXT NOT NULL DEFAULT '',
	namespace  TEXT NOT NULL DEFAULT '',
	detail     TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL DEFAULT (datetime('now'))
)`

// WriteAuditLog 写入审计日志
func (s *SQLiteStore) WriteAuditLog(_ context.Context, entry *domain.AuditLogEntry) error {
	_, err := s.db.Exec(
		`INSERT INTO audit_log (action, target_type, target_id, user_id, namespace, detail, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		entry.Action, entry.TargetType, entry.TargetID, entry.UserID, entry.Namespace, entry.Detail, time.Now().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("写入审计日志: %w", err)
	}
	return nil
}

// ListAuditLogs 分页查询审计日志
func (s *SQLiteStore) ListAuditLogs(_ context.Context, page, perPage int) ([]*domain.AuditLogEntry, int, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 50
	}

	// 查询总数
	var total int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM audit_log`).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("查询审计日志总数: %w", err)
	}

	// 分页查询
	offset := (page - 1) * perPage
	rows, err := s.db.Query(
		`SELECT id, action, target_type, target_id, user_id, namespace, detail, created_at
		 FROM audit_log ORDER BY id DESC LIMIT ? OFFSET ?`, perPage, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("查询审计日志: %w", err)
	}
	defer rows.Close()

	var entries []*domain.AuditLogEntry
	for rows.Next() {
		var e domain.AuditLogEntry
		var createdAt string
		if err := rows.Scan(&e.ID, &e.Action, &e.TargetType, &e.TargetID, &e.UserID, &e.Namespace, &e.Detail, &createdAt); err != nil {
			return nil, 0, fmt.Errorf("扫描审计日志: %w", err)
		}
		e.CreatedAt = createdAt
		entries = append(entries, &e)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return entries, total, nil
}

// AdminCountFiles 查询文件总数
func (s *SQLiteStore) AdminCountFiles(_ context.Context) (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM files`).Scan(&count)
	return count, err
}

// AdminCountBlobs 查询 blob 总数
func (s *SQLiteStore) AdminCountBlobs(_ context.Context) (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM content_blobs`).Scan(&count)
	return count, err
}

// AdminTotalBlobSize 查询所有 blob 的总大小
func (s *SQLiteStore) AdminTotalBlobSize(_ context.Context) (int64, error) {
	var total sql.NullInt64
	err := s.db.QueryRow(`SELECT SUM(size) FROM content_blobs`).Scan(&total)
	if err != nil {
		return 0, err
	}
	return total.Int64, nil
}
