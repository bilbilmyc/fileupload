package metadata

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"github.com/mayc/casdao/fileupload/internal/domain"
)

// SQLiteStore 冷数据存储：content_blobs + files（元数据持久化）
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore 创建 SQLiteStore，自动创建表
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("打开 SQLite: %w", err)
	}

	store := &SQLiteStore{db: db}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("迁移: %w", err)
	}
	return store, nil
}

// migrate 创建表结构
func (s *SQLiteStore) migrate() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS content_blobs (
			sha256       TEXT PRIMARY KEY,
			storage_path TEXT NOT NULL,
			size         BIGINT NOT NULL,
			ref_count    INTEGER NOT NULL DEFAULT 0,
			created_at   TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE TABLE IF NOT EXISTS files (
			file_id    TEXT PRIMARY KEY,
			sha256     TEXT REFERENCES content_blobs(sha256),
			name       TEXT NOT NULL,
			path       TEXT NOT NULL,
			size       BIGINT NOT NULL DEFAULT 0,
			namespace  TEXT NOT NULL,
			is_dir     INTEGER NOT NULL DEFAULT 0,
			parent_id  TEXT,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_files_namespace_parent ON files(namespace, parent_id)`,
		`CREATE INDEX IF NOT EXISTS idx_files_sha256 ON files(sha256)`,
		`CREATE INDEX IF NOT EXISTS idx_files_path ON files(namespace, path)`,
	}

	for _, q := range queries {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("执行迁移: %w", err)
		}
	}
	return nil
}

// ========== Content Blob ==========

// GetBlobBySha 按 SHA-256 查询去重记录
func (s *SQLiteStore) GetBlobBySha(_ context.Context, sha256 string) (*domain.ContentBlob, error) {
	row := s.db.QueryRow(
		`SELECT sha256, storage_path, size, ref_count, created_at FROM content_blobs WHERE sha256 = ?`,
		sha256,
	)

	var b domain.ContentBlob
	var createdAtStr string
	err := row.Scan(&b.SHA256, &b.StoragePath, &b.Size, &b.RefCount, &createdAtStr)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询 blob: %w", err)
	}
	b.CreatedAt = parseTime(createdAtStr)
	return &b, nil
}

// PutBlob 写入去重记录
func (s *SQLiteStore) PutBlob(_ context.Context, b *domain.ContentBlob) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO content_blobs (sha256, storage_path, size, ref_count, created_at) VALUES (?, ?, ?, ?, ?)`,
		b.SHA256, b.StoragePath, b.Size, b.RefCount, b.CreatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("写入 blob: %w", err)
	}
	return nil
}

// IncrBlobRef 增加引用计数
func (s *SQLiteStore) IncrBlobRef(_ context.Context, sha256 string) error {
	res, err := s.db.Exec(
		`UPDATE content_blobs SET ref_count = ref_count + 1 WHERE sha256 = ?`,
		sha256,
	)
	if err != nil {
		return fmt.Errorf("增加引用: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("blob 不存在: %s", sha256)
	}
	return nil
}

// DecrBlobRef 减少引用计数，返回新值
func (s *SQLiteStore) DecrBlobRef(_ context.Context, sha256 string) (int, error) {
	res, err := s.db.Exec(
		`UPDATE content_blobs SET ref_count = MAX(0, ref_count - 1) WHERE sha256 = ?`,
		sha256,
	)
	if err != nil {
		return 0, fmt.Errorf("减少引用: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return 0, nil
	}
	// 查新计数
	row := s.db.QueryRow(`SELECT ref_count FROM content_blobs WHERE sha256 = ?`, sha256)
	var count int
	if err := row.Scan(&count); err != nil {
		return 0, fmt.Errorf("查询新引用计数: %w", err)
	}
	return count, nil
}

// ========== File Metadata ==========

// PutFile 写入文件记录
func (s *SQLiteStore) PutFile(_ context.Context, f *domain.FileMetadata) error {
	_, err := s.db.Exec(
		`INSERT INTO files (file_id, sha256, name, path, size, namespace, is_dir, parent_id, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.FileID, nullStr(f.SHA256), f.Name, f.Path, f.Size, f.Namespace,
		boolToInt(f.IsDir), nullStr(f.ParentID), f.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("写入文件: %w", err)
	}
	return nil
}

// GetFile 按 ID 查文件
func (s *SQLiteStore) GetFile(_ context.Context, id string) (*domain.FileMetadata, error) {
	row := s.db.QueryRow(
		`SELECT file_id, sha256, name, path, size, namespace, is_dir, parent_id, created_at
		 FROM files WHERE file_id = ?`, id,
	)
	return scanFile(row)
}

// GetFileByPath 按 namespace + path 查文件
func (s *SQLiteStore) GetFileByPath(_ context.Context, namespace, path string) (*domain.FileMetadata, error) {
	row := s.db.QueryRow(
		`SELECT file_id, sha256, name, path, size, namespace, is_dir, parent_id, created_at
		 FROM files WHERE namespace = ? AND path = ?`, namespace, path,
	)
	return scanFile(row)
}

// ListChildren 列目录子节点
func (s *SQLiteStore) ListChildren(_ context.Context, parentID string) ([]*domain.FileMetadata, error) {
	rows, err := s.db.Query(
		`SELECT file_id, sha256, name, path, size, namespace, is_dir, parent_id, created_at
		 FROM files WHERE parent_id = ? ORDER BY name`, parentID,
	)
	if err != nil {
		return nil, fmt.Errorf("列子节点: %w", err)
	}
	defer rows.Close()
	return scanFiles(rows)
}

// DeleteFile 删除文件记录
func (s *SQLiteStore) DeleteFile(_ context.Context, id string) error {
	_, err := s.db.Exec(`DELETE FROM files WHERE file_id = ?`, id)
	return err
}

// ListFilesByBlob 查询引用同一 blob 的所有文件
func (s *SQLiteStore) ListFilesByBlob(_ context.Context, sha256 string) ([]*domain.FileMetadata, error) {
	rows, err := s.db.Query(
		`SELECT file_id, sha256, name, path, size, namespace, is_dir, parent_id, created_at
		 FROM files WHERE sha256 = ?`, sha256,
	)
	if err != nil {
		return nil, fmt.Errorf("按 blob 查文件: %w", err)
	}
	defer rows.Close()
	return scanFiles(rows)
}

// ListRoot 列 root 节点（parent_id IS NULL 且 namespace 匹配）
func (s *SQLiteStore) ListRoot(_ context.Context, namespace string) ([]*domain.FileMetadata, error) {
	rows, err := s.db.Query(
		`SELECT file_id, sha256, name, path, size, namespace, is_dir, parent_id, created_at
		 FROM files WHERE namespace = ? AND parent_id IS NULL ORDER BY name`, namespace,
	)
	if err != nil {
		return nil, fmt.Errorf("列根目录: %w", err)
	}
	defer rows.Close()
	return scanFiles(rows)
}

// ========== 一致性巡检 ==========

func (s *SQLiteStore) ListAllBlobs(_ context.Context) ([]*domain.ContentBlob, error) {
	rows, err := s.db.Query(
		`SELECT sha256, storage_path, size, ref_count, created_at FROM content_blobs`,
	)
	if err != nil {
		return nil, fmt.Errorf("列举全部 blob: %w", err)
	}
	defer rows.Close()

	var blobs []*domain.ContentBlob
	for rows.Next() {
		var b domain.ContentBlob
		var createdAtStr string
		if err := rows.Scan(&b.SHA256, &b.StoragePath, &b.Size, &b.RefCount, &createdAtStr); err != nil {
			return nil, fmt.Errorf("扫描 blob: %w", err)
		}
		b.CreatedAt = parseTime(createdAtStr)
		blobs = append(blobs, &b)
	}
	return blobs, rows.Err()
}

func (s *SQLiteStore) ListAllFiles(_ context.Context) ([]*domain.FileMetadata, error) {
	rows, err := s.db.Query(
		`SELECT file_id, sha256, name, path, size, namespace, is_dir, parent_id, created_at
		 FROM files ORDER BY file_id`,
	)
	if err != nil {
		return nil, fmt.Errorf("列举全部文件: %w", err)
	}
	defer rows.Close()
	return scanFiles(rows)
}

// Close 关闭数据库连接
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// ========== 辅助函数 ==========

func scanFile(row *sql.Row) (*domain.FileMetadata, error) {
	var f domain.FileMetadata
	var sha256, parentID, createdAtStr sql.NullString
	var isDir int64
	err := row.Scan(&f.FileID, &sha256, &f.Name, &f.Path, &f.Size, &f.Namespace, &isDir, &parentID, &createdAtStr)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("扫描文件: %w", err)
	}
	f.SHA256 = sha256.String
	f.ParentID = parentID.String
	f.IsDir = isDir != 0
	f.CreatedAt = parseTime(createdAtStr.String)
	return &f, nil
}

func scanFiles(rows *sql.Rows) ([]*domain.FileMetadata, error) {
	var files []*domain.FileMetadata
	for rows.Next() {
		var f domain.FileMetadata
		var sha256, parentID, createdAtStr sql.NullString
		var isDir int64
		if err := rows.Scan(&f.FileID, &sha256, &f.Name, &f.Path, &f.Size, &f.Namespace, &isDir, &parentID, &createdAtStr); err != nil {
			return nil, fmt.Errorf("扫描文件: %w", err)
		}
		f.SHA256 = sha256.String
		f.ParentID = parentID.String
		f.IsDir = isDir != 0
		f.CreatedAt = parseTime(createdAtStr.String)
		files = append(files, &f)
	}
	return files, rows.Err()
}

func nullStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	// 尝试多种格式
	for _, fmt := range []string{
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02 15:04:05.999999 -0700 MST",
	} {
		t, err := time.Parse(fmt, s)
		if err == nil {
			return t
		}
	}
	// 如果无法解析，返回当前时间作为 fallback
	return time.Now()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// compile-time assertion
var _ domain.Metadata = (*Facade)(nil)
