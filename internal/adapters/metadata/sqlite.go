package metadata

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"github.com/bilbilmyc/fileupload/internal/domain"
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
		`CREATE TABLE IF NOT EXISTS file_tags (
			file_id    TEXT NOT NULL REFERENCES files(file_id) ON DELETE CASCADE,
			tag        TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			PRIMARY KEY (file_id, tag)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_file_tags_tag ON file_tags(tag)`,
		auditLogMigration,
		`CREATE TABLE IF NOT EXISTS shares (
			token        TEXT PRIMARY KEY,
			file_id      TEXT NOT NULL,
			password_hash TEXT NOT NULL DEFAULT '',
			expires_at   TEXT NOT NULL DEFAULT '',
			max_downloads INTEGER NOT NULL DEFAULT 0,
			cur_downloads INTEGER NOT NULL DEFAULT 0,
			namespace    TEXT NOT NULL DEFAULT '',
			created_at   TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
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
func (s *SQLiteStore) UpdateBlobStorage(_ context.Context, sha256 string, storagePath string) error {
	_, err := s.db.Exec(`UPDATE content_blobs SET storage_path = ? WHERE sha256 = ?`, storagePath, sha256)
	return err
}

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

// ListChildren 列目录子节点，支持可选搜索
func (s *SQLiteStore) ListChildrenPage(ctx context.Context, parentID string, search string, page, perPage int, sortBy, sortOrder string) ([]*domain.FileMetadata, int, error) {
	// Validate sort
	safeSort := "name"
	switch sortBy {
	case "name", "size", "created_at":
		safeSort = sortBy
	}
	order := "ASC"
	if sortOrder == "desc" {
		order = "DESC"
	}
	offset := (page - 1) * perPage
	if offset < 0 { offset = 0 }

	// Count
	var total int
	countSQL := "SELECT COUNT(*) FROM files WHERE parent_id=?"
	args := []any{parentID}
	if search != "" {
		countSQL += " AND name LIKE ?"
		args = append(args, "%"+search+"%")
	}
	s.db.QueryRowContext(ctx, countSQL, args...).Scan(&total)

	// Query with limit
	sql := fmt.Sprintf("SELECT file_id, sha256, name, path, size, namespace, is_dir, parent_id, created_at FROM files WHERE parent_id=?")
	searchArgs := []any{parentID}
	if search != "" {
		sql += " AND name LIKE ?"
		searchArgs = append(searchArgs, "%"+search+"%")
	}
	sql += fmt.Sprintf(" ORDER BY %s %s LIMIT ? OFFSET ?", safeSort, order)
	searchArgs = append(searchArgs, perPage, offset)
	rows, err := s.db.QueryContext(ctx, sql, searchArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	files, err := scanFiles(rows)
	if err != nil { return nil, 0, err }
	return files, total, nil
}

func (s *SQLiteStore) ListChildren(_ context.Context, parentID string, search string) ([]*domain.FileMetadata, error) {
	var rows *sql.Rows
	var err error
	if search != "" {
		rows, err = s.db.Query(
			`SELECT file_id, sha256, name, path, size, namespace, is_dir, parent_id, created_at
			 FROM files WHERE parent_id = ? AND name LIKE ? ORDER BY name`,
			parentID, "%"+search+"%",
		)
	} else {
		rows, err = s.db.Query(
			`SELECT file_id, sha256, name, path, size, namespace, is_dir, parent_id, created_at
			 FROM files WHERE parent_id = ? ORDER BY name`, parentID,
		)
	}
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

// ListRoot 列 root 节点（parent_id IS NULL 且 namespace 匹配），支持可选搜索
func (s *SQLiteStore) ListRootPage(ctx context.Context, namespace string, search string, page, perPage int, sortBy, sortOrder string) ([]*domain.FileMetadata, int, error) {
	safeSort := "name"
	switch sortBy {
	case "name", "size", "created_at":
		safeSort = sortBy
	}
	order := "ASC"
	if sortOrder == "desc" {
		order = "DESC"
	}
	offset := (page - 1) * perPage
	if offset < 0 { offset = 0 }

	var total int
	countSQL := "SELECT COUNT(*) FROM files WHERE (parent_id IS NULL OR parent_id='') AND namespace=?"
	args := []any{namespace}
	if search != "" {
		countSQL += " AND name LIKE ?"
		args = append(args, "%"+search+"%")
	}
	s.db.QueryRowContext(ctx, countSQL, args...).Scan(&total)

	sql := fmt.Sprintf("SELECT file_id, sha256, name, path, size, namespace, is_dir, parent_id, created_at FROM files WHERE (parent_id IS NULL OR parent_id='') AND namespace=?")
	searchArgs := []any{namespace}
	if search != "" {
		sql += " AND name LIKE ?"
		searchArgs = append(searchArgs, "%"+search+"%")
	}
	sql += fmt.Sprintf(" ORDER BY %s %s LIMIT ? OFFSET ?", safeSort, order)
	searchArgs = append(searchArgs, perPage, offset)
	rows, err := s.db.QueryContext(ctx, sql, searchArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	files, err := scanFiles(rows)
	if err != nil { return nil, 0, err }
	return files, total, nil
}

func (s *SQLiteStore) ListRoot(_ context.Context, namespace string, search string) ([]*domain.FileMetadata, error) {
	var rows *sql.Rows
	var err error
	if search != "" {
		rows, err = s.db.Query(
			`SELECT file_id, sha256, name, path, size, namespace, is_dir, parent_id, created_at
			 FROM files WHERE namespace = ? AND parent_id IS NULL AND name LIKE ? ORDER BY name`,
			namespace, "%"+search+"%",
		)
	} else {
		rows, err = s.db.Query(
			`SELECT file_id, sha256, name, path, size, namespace, is_dir, parent_id, created_at
			 FROM files WHERE namespace = ? AND parent_id IS NULL ORDER BY name`, namespace,
		)
	}
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

// ========== 标签管理 ==========

func (s *SQLiteStore) RenameFile(_ context.Context, fileID, newName, newPath string) error {
	_, err := s.db.ExecContext(context.Background(), "UPDATE files SET name=?, path=? WHERE file_id=?", newName, newPath, fileID)
	return err
}

func (s *SQLiteStore) SetFileTags(_ context.Context, fileID string, tags []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("开启事务: %w", err)
	}
	defer tx.Rollback()

	// 清除旧标签
	if _, err := tx.Exec(`DELETE FROM file_tags WHERE file_id = ?`, fileID); err != nil {
		return fmt.Errorf("清除旧标签: %w", err)
	}

	// 写入新标签
	for _, tag := range tags {
		if tag == "" {
			continue
		}
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO file_tags (file_id, tag, created_at) VALUES (?, ?, datetime('now'))`,
			fileID, tag,
		); err != nil {
			return fmt.Errorf("写入标签 %s: %w", tag, err)
		}
	}

	return tx.Commit()
}

func (s *SQLiteStore) GetFileTags(_ context.Context, fileID string) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT tag FROM file_tags WHERE file_id = ? ORDER BY tag`, fileID,
	)
	if err != nil {
		return nil, fmt.Errorf("查询标签: %w", err)
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, fmt.Errorf("扫描标签: %w", err)
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

func (s *SQLiteStore) DeleteFileTags(_ context.Context, fileID string) error {
	_, err := s.db.Exec(`DELETE FROM file_tags WHERE file_id = ?`, fileID)
	return err
}

// ========== 批量管理 ==========

func (s *SQLiteStore) ReparentFile(_ context.Context, fileID string, parentID *string, path string) error {
	var res error
	if parentID == nil {
		_, res = s.db.Exec(`UPDATE files SET parent_id = NULL, path = ? WHERE file_id = ?`, path, fileID)
	} else {
		_, res = s.db.Exec(`UPDATE files SET parent_id = ?, path = ? WHERE file_id = ?`, *parentID, path, fileID)
	}
	return res
}

func (s *SQLiteStore) UpdateFileParent(_ context.Context, fileID string, parentID *string) error {
	var res error
	if parentID == nil {
		_, res = s.db.Exec(`UPDATE files SET parent_id = NULL WHERE file_id = ?`, fileID)
	} else {
		_, res = s.db.Exec(`UPDATE files SET parent_id = ? WHERE file_id = ?`, *parentID, fileID)
	}
	return res
}

// ========== 分享链接 ==========

func (s *SQLiteStore) CreateShare(_ context.Context, token string, entry *domain.ShareEntry) error {
	_, err := s.db.Exec(
		`INSERT INTO shares (token, file_id, password_hash, expires_at, max_downloads, cur_downloads, namespace, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		token, entry.FileID, entry.PasswordHash, entry.ExpiresAt, entry.MaxDownloads, entry.CurDownloads, entry.Namespace,
	)
	return err
}

func (s *SQLiteStore) GetShare(_ context.Context, token string) (*domain.ShareEntry, error) {
	row := s.db.QueryRow(`SELECT token, file_id, password_hash, expires_at, max_downloads, cur_downloads, namespace, created_at FROM shares WHERE token = ?`, token)
	var e domain.ShareEntry
	var createdAt string
	err := row.Scan(&e.Token, &e.FileID, &e.PasswordHash, &e.ExpiresAt, &e.MaxDownloads, &e.CurDownloads, &e.Namespace, &createdAt)
	if err != nil {
		return nil, nil
	}
	e.CreatedAt = createdAt
	return &e, nil
}

func (s *SQLiteStore) IncrDownloads(_ context.Context, token string) error {
	_, err := s.db.Exec(`UPDATE shares SET cur_downloads = cur_downloads + 1 WHERE token = ?`, token)
	return err
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
