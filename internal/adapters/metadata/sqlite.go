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

// migrate 按版本应用 SQLite schema 迁移。
func (s *SQLiteStore) migrate() error {
	return runSQLiteMigrations(s.db)
}

// ReserveNamespaceBytes atomically accounts for active files and outstanding reservations.
func (s *SQLiteStore) ReserveNamespaceBytes(ctx context.Context, namespace, reservationID string, bytes, quota int64) error {
	if namespace == "" || reservationID == "" || bytes < 0 {
		return domain.ErrInvalidArgument
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启配额事务: %w", err)
	}
	defer tx.Rollback()
	// The lock row serializes reservations for the same namespace under SQLite's
	// single-writer model, while keeping different namespaces independent logically.
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO namespace_quota_locks(namespace) VALUES (?)`, namespace); err != nil {
		return fmt.Errorf("锁定命名空间配额: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE namespace_quota_locks SET namespace = namespace WHERE namespace = ?`, namespace); err != nil {
		return fmt.Errorf("锁定命名空间配额: %w", err)
	}
	var used, reserved, current int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(size), 0) FROM files WHERE namespace = ? AND deleted_at IS NULL`, namespace).Scan(&used); err != nil {
		return fmt.Errorf("读取命名空间用量: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(bytes), 0) FROM namespace_reservations WHERE namespace = ?`, namespace).Scan(&reserved); err != nil {
		return fmt.Errorf("读取命名空间预留: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(bytes, 0) FROM namespace_reservations WHERE reservation_id = ?`, reservationID).Scan(&current); err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("读取当前配额预留: %w", err)
	}
	if quota > 0 && used+reserved-current+bytes > quota {
		return domain.ErrQuotaExceeded
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO namespace_reservations(reservation_id, namespace, bytes) VALUES (?, ?, ?) ON CONFLICT(reservation_id) DO UPDATE SET namespace = excluded.namespace, bytes = excluded.bytes`, reservationID, namespace, bytes); err != nil {
		return fmt.Errorf("写入命名空间预留: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交配额预留: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ReleaseNamespaceReservation(ctx context.Context, reservationID string) error {
	if reservationID == "" {
		return domain.ErrInvalidArgument
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM namespace_reservations WHERE reservation_id = ?`, reservationID)
	if err != nil {
		return fmt.Errorf("释放命名空间预留: %w", err)
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

// GetBlobsBySha 批量查询去重记录，供批量下载减少数据库往返。
func (s *SQLiteStore) GetBlobsBySha(ctx context.Context, sha256s []string) (map[string]*domain.ContentBlob, error) {
	values := uniqueNonEmpty(sha256s)
	result := make(map[string]*domain.ContentBlob, len(values))
	for start := 0; start < len(values); start += batchQueryLimit {
		end := start + batchQueryLimit
		if end > len(values) {
			end = len(values)
		}
		batch := values[start:end]
		args := make([]any, len(batch))
		for i, sha256 := range batch {
			args[i] = sha256
		}
		query := fmt.Sprintf(`SELECT sha256, storage_path, size, ref_count, created_at
			FROM content_blobs WHERE sha256 IN (%s)`, sqlitePlaceholders(len(batch)))
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("批量查询 blob: %w", err)
		}
		for rows.Next() {
			var blob domain.ContentBlob
			var createdAt string
			if err := rows.Scan(&blob.SHA256, &blob.StoragePath, &blob.Size, &blob.RefCount, &createdAt); err != nil {
				rows.Close()
				return nil, fmt.Errorf("扫描 blob: %w", err)
			}
			blob.CreatedAt = parseTime(createdAt)
			result[blob.SHA256] = &blob
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("读取 blob: %w", err)
		}
		rows.Close()
	}
	return result, nil
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

// AcquireBlob 原子创建或递增 blob 引用，避免相同 SHA 并发提交丢失引用。
func (s *SQLiteStore) AcquireBlob(ctx context.Context, b *domain.ContentBlob) (string, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", false, fmt.Errorf("开启 blob 事务: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO content_blobs (sha256, storage_path, size, ref_count, created_at) VALUES (?, ?, ?, 1, ?)`,
		b.SHA256, b.StoragePath, b.Size, b.CreatedAt.Format(time.RFC3339))
	if err != nil {
		return "", false, fmt.Errorf("创建 blob: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE content_blobs SET ref_count = ref_count + 1 WHERE sha256 = ?`, b.SHA256); err != nil {
			return "", false, fmt.Errorf("增加 blob 引用: %w", err)
		}
		var canonicalPath string
		if err := tx.QueryRowContext(ctx, `SELECT storage_path FROM content_blobs WHERE sha256 = ?`, b.SHA256).Scan(&canonicalPath); err != nil {
			return "", false, fmt.Errorf("读取 blob 存储路径: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return "", false, err
		}
		return canonicalPath, false, nil
	}
	if err := tx.Commit(); err != nil {
		return "", false, err
	}
	return b.StoragePath, true, nil
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

// GetFilesByIDs 批量查询未删除文件，供批量下载减少数据库往返。
func (s *SQLiteStore) GetFilesByIDs(ctx context.Context, ids []string) ([]*domain.FileMetadata, error) {
	values := uniqueNonEmpty(ids)
	files := make([]*domain.FileMetadata, 0, len(values))
	for start := 0; start < len(values); start += batchQueryLimit {
		end := start + batchQueryLimit
		if end > len(values) {
			end = len(values)
		}
		batch := values[start:end]
		args := make([]any, len(batch))
		for i, id := range batch {
			args[i] = id
		}
		query := fmt.Sprintf(`SELECT file_id, sha256, name, path, size, namespace, is_dir, parent_id, created_at
			FROM files WHERE file_id IN (%s) AND deleted_at IS NULL`, sqlitePlaceholders(len(batch)))
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("批量查询文件: %w", err)
		}
		batchFiles, scanErr := scanFiles(rows)
		rows.Close()
		if scanErr != nil {
			return nil, scanErr
		}
		files = append(files, batchFiles...)
	}
	return files, nil
}

// GetFile 按 ID 查文件
func (s *SQLiteStore) GetFile(_ context.Context, id string) (*domain.FileMetadata, error) {
	row := s.db.QueryRow(
		`SELECT file_id, sha256, name, path, size, namespace, is_dir, parent_id, created_at
		 FROM files WHERE file_id = ? AND deleted_at IS NULL`, id,
	)
	return scanFile(row)
}

// GetFileByPath 按 namespace + path 查文件
func (s *SQLiteStore) GetFileByPath(_ context.Context, namespace, path string) (*domain.FileMetadata, error) {
	row := s.db.QueryRow(
		`SELECT file_id, sha256, name, path, size, namespace, is_dir, parent_id, created_at
		 FROM files WHERE namespace = ? AND path = ? AND deleted_at IS NULL`, namespace, path,
	)
	return scanFile(row)
}

// sqliteFileTypeClause returns a fixed SQL predicate for the supported list filters.
// It never interpolates request data into SQL.
func sqliteFileTypeClause(fileType string) string {
	switch fileType {
	case "dir":
		return " AND is_dir = 1"
	case "file":
		return " AND is_dir = 0"
	default:
		return ""
	}
}

// ListChildren 列目录子节点，支持可选搜索
func (s *SQLiteStore) ListChildrenPage(ctx context.Context, parentID string, search, fileType string, page, perPage int, sortBy, sortOrder string) ([]*domain.FileMetadata, int, error) {
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
	if offset < 0 {
		offset = 0
	}

	// Count
	var total int
	countSQL := "SELECT COUNT(*) FROM files WHERE parent_id=? AND deleted_at IS NULL"
	args := []any{parentID}
	if search != "" {
		countSQL += " AND name LIKE ?"
		args = append(args, "%"+search+"%")
	}
	countSQL += sqliteFileTypeClause(fileType)
	if err := s.db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Query with limit
	sql := fmt.Sprintf("SELECT file_id, sha256, name, path, size, namespace, is_dir, parent_id, created_at FROM files WHERE parent_id=? AND deleted_at IS NULL")
	searchArgs := []any{parentID}
	if search != "" {
		sql += " AND name LIKE ?"
		searchArgs = append(searchArgs, "%"+search+"%")
	}
	sql += sqliteFileTypeClause(fileType)
	sql += fmt.Sprintf(" ORDER BY %s %s, file_id ASC LIMIT ? OFFSET ?", safeSort, order)
	searchArgs = append(searchArgs, perPage, offset)
	rows, err := s.db.QueryContext(ctx, sql, searchArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	files, err := scanFiles(rows)
	if err != nil {
		return nil, 0, err
	}
	return files, total, nil
}

func (s *SQLiteStore) ListChildren(_ context.Context, parentID string, search string) ([]*domain.FileMetadata, error) {
	var rows *sql.Rows
	var err error
	if search != "" {
		rows, err = s.db.Query(
			`SELECT file_id, sha256, name, path, size, namespace, is_dir, parent_id, created_at
			 FROM files WHERE parent_id = ? AND deleted_at IS NULL AND name LIKE ? ORDER BY name`,
			parentID, "%"+search+"%",
		)
	} else {
		rows, err = s.db.Query(
			`SELECT file_id, sha256, name, path, size, namespace, is_dir, parent_id, created_at
			 FROM files WHERE parent_id = ? AND deleted_at IS NULL ORDER BY name`, parentID,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("列子节点: %w", err)
	}
	defer rows.Close()
	return scanFiles(rows)
}

// GetNamespaceUsage 按 namespace 聚合逻辑文件的容量与数量；目录不占容量。
func (s *SQLiteStore) GetNamespaceUsage(ctx context.Context, namespace string) (*domain.NamespaceUsage, error) {
	usage := &domain.NamespaceUsage{}
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(size), 0) FROM files WHERE namespace = ? AND is_dir = 0 AND deleted_at IS NULL`, namespace).Scan(&usage.FileCount, &usage.TotalSize)
	if err != nil {
		return nil, err
	}
	return usage, nil
}

// ListTrash 返回命名空间中已软删除的节点（保留原父子关系，供恢复与彻底删除使用）。
func (s *SQLiteStore) ListTrash(ctx context.Context, namespace string) ([]*domain.FileMetadata, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT file_id, sha256, name, path, size, namespace, is_dir, parent_id, created_at, deleted_at FROM files WHERE namespace = ? AND deleted_at IS NOT NULL ORDER BY deleted_at DESC, name ASC`, namespace)
	if err != nil {
		return nil, fmt.Errorf("列回收站: %w", err)
	}
	defer rows.Close()
	var files []*domain.FileMetadata
	for rows.Next() {
		var f domain.FileMetadata
		var sha, parent, created, deleted sql.NullString
		var isDir int64
		if err := rows.Scan(&f.FileID, &sha, &f.Name, &f.Path, &f.Size, &f.Namespace, &isDir, &parent, &created, &deleted); err != nil {
			return nil, fmt.Errorf("扫描回收站文件: %w", err)
		}
		f.SHA256, f.ParentID, f.IsDir, f.CreatedAt = sha.String, parent.String, isDir != 0, parseTime(created.String)
		if deleted.Valid {
			value := parseTime(deleted.String)
			f.DeletedAt = &value
		}
		files = append(files, &f)
	}
	return files, rows.Err()
}

func (s *SQLiteStore) MoveFileToTrash(ctx context.Context, id string, deletedAt time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE files SET deleted_at = ? WHERE file_id = ? AND deleted_at IS NULL`, deletedAt.UTC().Format(time.RFC3339), id)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) RestoreFile(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE files SET deleted_at = NULL WHERE file_id = ? AND deleted_at IS NOT NULL`, id)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return domain.ErrNotFound
	}
	return nil
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
func (s *SQLiteStore) ListRootPage(ctx context.Context, namespace string, search, fileType string, page, perPage int, sortBy, sortOrder string) ([]*domain.FileMetadata, int, error) {
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
	if offset < 0 {
		offset = 0
	}

	var total int
	countSQL := "SELECT COUNT(*) FROM files WHERE (parent_id IS NULL OR parent_id='') AND namespace=? AND deleted_at IS NULL"
	args := []any{namespace}
	if search != "" {
		countSQL += " AND name LIKE ?"
		args = append(args, "%"+search+"%")
	}
	countSQL += sqliteFileTypeClause(fileType)
	if err := s.db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	sql := fmt.Sprintf("SELECT file_id, sha256, name, path, size, namespace, is_dir, parent_id, created_at FROM files WHERE (parent_id IS NULL OR parent_id='') AND namespace=? AND deleted_at IS NULL")
	searchArgs := []any{namespace}
	if search != "" {
		sql += " AND name LIKE ?"
		searchArgs = append(searchArgs, "%"+search+"%")
	}
	sql += sqliteFileTypeClause(fileType)
	sql += fmt.Sprintf(" ORDER BY %s %s, file_id ASC LIMIT ? OFFSET ?", safeSort, order)
	searchArgs = append(searchArgs, perPage, offset)
	rows, err := s.db.QueryContext(ctx, sql, searchArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	files, err := scanFiles(rows)
	if err != nil {
		return nil, 0, err
	}
	return files, total, nil
}

func (s *SQLiteStore) ListRoot(_ context.Context, namespace string, search string) ([]*domain.FileMetadata, error) {
	var rows *sql.Rows
	var err error
	if search != "" {
		rows, err = s.db.Query(
			`SELECT file_id, sha256, name, path, size, namespace, is_dir, parent_id, created_at
			 FROM files WHERE namespace = ? AND parent_id IS NULL AND deleted_at IS NULL AND name LIKE ? ORDER BY name`,
			namespace, "%"+search+"%",
		)
	} else {
		rows, err = s.db.Query(
			`SELECT file_id, sha256, name, path, size, namespace, is_dir, parent_id, created_at
			 FROM files WHERE namespace = ? AND parent_id IS NULL AND deleted_at IS NULL ORDER BY name`, namespace,
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
		`SELECT file_id, sha256, name, path, size, namespace, is_dir, parent_id, created_at, deleted_at
		 FROM files ORDER BY file_id`,
	)
	if err != nil {
		return nil, fmt.Errorf("列举全部文件: %w", err)
	}
	defer rows.Close()
	return scanFilesWithDeleted(rows)
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

func (s *SQLiteStore) ListShares(_ context.Context, namespace, fileID string) ([]*domain.ShareEntry, error) {
	query := `SELECT token, file_id, password_hash, expires_at, max_downloads, cur_downloads, namespace, created_at FROM shares WHERE namespace = ?`
	args := []any{namespace}
	if fileID != "" {
		query += ` AND file_id = ?`
		args = append(args, fileID)
	}
	query += ` ORDER BY created_at DESC`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := make([]*domain.ShareEntry, 0)
	for rows.Next() {
		entry, err := scanShare(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (s *SQLiteStore) DeleteShare(_ context.Context, token, namespace string) error {
	result, err := s.db.Exec(`DELETE FROM shares WHERE token = ? AND namespace = ?`, token, namespace)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) IncrDownloads(_ context.Context, token string) error {
	_, err := s.db.Exec(`UPDATE shares SET cur_downloads = cur_downloads + 1 WHERE token = ?`, token)
	return err
}

// TryConsumeDownload 原子检查并消耗一次分享下载额度。
func (s *SQLiteStore) TryConsumeDownload(ctx context.Context, token string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE shares SET cur_downloads = cur_downloads + 1 WHERE token = ? AND (max_downloads = 0 OR cur_downloads < max_downloads)`, token)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// Close 关闭数据库连接
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// ========== 辅助函数 ==========

type shareScanner interface {
	Scan(dest ...any) error
}

func scanShare(row shareScanner) (*domain.ShareEntry, error) {
	var entry domain.ShareEntry
	var createdAt string
	if err := row.Scan(&entry.Token, &entry.FileID, &entry.PasswordHash, &entry.ExpiresAt, &entry.MaxDownloads, &entry.CurDownloads, &entry.Namespace, &createdAt); err != nil {
		return nil, err
	}
	entry.CreatedAt = createdAt
	entry.PasswordProtected = entry.PasswordHash != ""
	return &entry, nil
}

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

func scanFilesWithDeleted(rows *sql.Rows) ([]*domain.FileMetadata, error) {
	var files []*domain.FileMetadata
	for rows.Next() {
		var f domain.FileMetadata
		var sha256, parentID, createdAt, deletedAt sql.NullString
		var isDir int64
		if err := rows.Scan(&f.FileID, &sha256, &f.Name, &f.Path, &f.Size, &f.Namespace, &isDir, &parentID, &createdAt, &deletedAt); err != nil {
			return nil, fmt.Errorf("扫描文件: %w", err)
		}
		f.SHA256, f.ParentID, f.IsDir, f.CreatedAt = sha256.String, parentID.String, isDir != 0, parseTime(createdAt.String)
		if deletedAt.Valid {
			value := parseTime(deletedAt.String)
			f.DeletedAt = &value
		}
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

// HealthCheck 检查 SQLite 连接。
func (s *SQLiteStore) HealthCheck(ctx context.Context) error {
	return s.db.PingContext(ctx)
}
