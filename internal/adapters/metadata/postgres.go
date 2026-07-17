package metadata

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

// PostgresStore PostgreSQL 元数据存储
// 实现 domain.Metadata 端口，用于生产环境多实例共享元数据。
type PostgresStore struct {
	db *sql.DB
}

// NewPostgresStore 创建 PostgresStore
// dsn 格式: postgres://user:password@host:port/dbname?sslmode=disable
func NewPostgresStore(dsn string) (*PostgresStore, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开 PostgreSQL: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	store := &PostgresStore{db: db}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("迁移: %w", err)
	}
	return store, nil
}

// migrate 创建表结构（与 SQLite 迁移对应）
func (s *PostgresStore) migrate() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS content_blobs (
			sha256       TEXT PRIMARY KEY,
			storage_path TEXT NOT NULL,
			size         BIGINT NOT NULL,
			ref_count    INTEGER NOT NULL DEFAULT 0,
			created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS files (
			file_id    TEXT PRIMARY KEY,
			sha256     TEXT REFERENCES content_blobs(sha256),
			name       TEXT NOT NULL,
			path       TEXT NOT NULL DEFAULT '',
			size       BIGINT NOT NULL DEFAULT 0,
			namespace  TEXT NOT NULL,
			is_dir     BOOLEAN NOT NULL DEFAULT FALSE,
			parent_id  TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			deleted_at TIMESTAMPTZ
		)`,
		`CREATE INDEX IF NOT EXISTS idx_files_namespace_parent ON files(namespace, parent_id)`,
		`CREATE INDEX IF NOT EXISTS idx_files_sha256 ON files(sha256)`,
		`CREATE INDEX IF NOT EXISTS idx_files_path ON files(namespace, path)`,
		`CREATE TABLE IF NOT EXISTS file_tags (
			file_id    TEXT NOT NULL REFERENCES files(file_id) ON DELETE CASCADE,
			tag        TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (file_id, tag)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_file_tags_tag ON file_tags(tag)`,
		`CREATE TABLE IF NOT EXISTS audit_log (
			id         SERIAL PRIMARY KEY,
			action     TEXT NOT NULL,
			target_type TEXT NOT NULL DEFAULT '',
			target_id  TEXT NOT NULL DEFAULT '',
			user_id    TEXT NOT NULL DEFAULT '',
			namespace  TEXT NOT NULL DEFAULT '',
			detail     TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS shares (
			token        TEXT PRIMARY KEY,
			file_id      TEXT NOT NULL,
			password_hash TEXT NOT NULL DEFAULT '',
			expires_at   TEXT NOT NULL DEFAULT '',
			max_downloads INTEGER NOT NULL DEFAULT 0,
			cur_downloads INTEGER NOT NULL DEFAULT 0,
			namespace    TEXT NOT NULL DEFAULT '',
			created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
	}

	for _, q := range queries {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("执行迁移: %w", err)
		}
	}
	// 支持既有实例在线升级到回收站软删除字段。
	if _, err := s.db.Exec(`ALTER TABLE files ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ`); err != nil {
		return fmt.Errorf("添加 files.deleted_at: %w", err)
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_files_namespace_deleted ON files(namespace, deleted_at)`); err != nil {
		return fmt.Errorf("创建回收站索引: %w", err)
	}
	return nil
}

// ========== Content Blob ==========

func (s *PostgresStore) GetBlobBySha(ctx context.Context, sha256 string) (*domain.ContentBlob, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT sha256, storage_path, size, ref_count, created_at FROM content_blobs WHERE sha256 = $1`, sha256)
	return scanBlob(row)
}

func (s *PostgresStore) PutBlob(ctx context.Context, b *domain.ContentBlob) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO content_blobs (sha256, storage_path, size, ref_count, created_at) VALUES ($1, $2, $3, $4, $5) ON CONFLICT (sha256) DO NOTHING`,
		b.SHA256, b.StoragePath, b.Size, b.RefCount, b.CreatedAt)
	return err
}

func (s *PostgresStore) UpdateBlobStorage(ctx context.Context, sha256 string, storagePath string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE content_blobs SET storage_path = $1 WHERE sha256 = $2`, storagePath, sha256)
	return err
}

func (s *PostgresStore) IncrBlobRef(ctx context.Context, sha256 string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE content_blobs SET ref_count = ref_count + 1 WHERE sha256 = $1`, sha256)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("blob 不存在: %s", sha256)
	}
	return nil
}

func (s *PostgresStore) DecrBlobRef(ctx context.Context, sha256 string) (int, error) {
	_, err := s.db.ExecContext(ctx, `UPDATE content_blobs SET ref_count = GREATEST(0, ref_count - 1) WHERE sha256 = $1`, sha256)
	if err != nil {
		return 0, err
	}
	var count int
	err = s.db.QueryRowContext(ctx, `SELECT ref_count FROM content_blobs WHERE sha256 = $1`, sha256).Scan(&count)
	return count, err
}

// ========== File Metadata ==========

func (s *PostgresStore) PutFile(ctx context.Context, f *domain.FileMetadata) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO files (file_id, sha256, name, path, size, namespace, is_dir, parent_id, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		f.FileID, nullStr(f.SHA256), f.Name, f.Path, f.Size, f.Namespace, f.IsDir, nullStr(f.ParentID), f.CreatedAt)
	return err
}

func (s *PostgresStore) GetFile(ctx context.Context, id string) (*domain.FileMetadata, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT file_id, sha256, name, path, size, namespace, is_dir, parent_id, created_at FROM files WHERE file_id = $1 AND deleted_at IS NULL`, id)
	return scanFilePG(row)
}

func (s *PostgresStore) GetFileByPath(ctx context.Context, namespace, path string) (*domain.FileMetadata, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT file_id, sha256, name, path, size, namespace, is_dir, parent_id, created_at FROM files WHERE namespace = $1 AND path = $2 AND deleted_at IS NULL`,
		namespace, path)
	return scanFilePG(row)
}

func postgresFileTypeClause(fileType string) string {
	switch fileType {
	case "dir":
		return " AND is_dir = TRUE"
	case "file":
		return " AND is_dir = FALSE"
	default:
		return ""
	}
}

func (s *PostgresStore) ListChildrenPage(ctx context.Context, parentID string, search, fileType string, page, perPage int, sortBy, sortOrder string) ([]*domain.FileMetadata, int, error) {
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
	countSQL := "SELECT COUNT(*) FROM files WHERE parent_id=$1 AND deleted_at IS NULL"
	countArgs := []any{parentID}
	if search != "" {
		countSQL += " AND name ILIKE $2"
		countArgs = append(countArgs, "%"+search+"%")
	}
	countSQL += postgresFileTypeClause(fileType)
	if err := s.db.QueryRowContext(ctx, countSQL, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}
	sql := "SELECT file_id, sha256, name, path, size, namespace, is_dir, parent_id, created_at FROM files WHERE parent_id=$1 AND deleted_at IS NULL"
	queryArgs := []any{parentID}
	paramIdx := 2
	if search != "" {
		sql += fmt.Sprintf(" AND name ILIKE $%d", paramIdx)
		queryArgs = append(queryArgs, "%"+search+"%")
		paramIdx++
	}
	sql += postgresFileTypeClause(fileType)
	sql += fmt.Sprintf(" ORDER BY %s %s, file_id ASC LIMIT $%d OFFSET $%d", safeSort, order, paramIdx, paramIdx+1)
	queryArgs = append(queryArgs, perPage, offset)
	rows, err := s.db.QueryContext(ctx, sql, queryArgs...)
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

func (s *PostgresStore) ListChildren(ctx context.Context, parentID string, search string) ([]*domain.FileMetadata, error) {
	var rows *sql.Rows
	var err error
	if search != "" {
		rows, err = s.db.QueryContext(ctx,
			`SELECT file_id, sha256, name, path, size, namespace, is_dir, parent_id, created_at FROM files WHERE parent_id = $1 AND deleted_at IS NULL AND name ILIKE $2 ORDER BY name`,
			parentID, "%"+search+"%")
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT file_id, sha256, name, path, size, namespace, is_dir, parent_id, created_at FROM files WHERE parent_id = $1 AND deleted_at IS NULL ORDER BY name`,
			parentID)
	}
	if err != nil {
		return nil, fmt.Errorf("列子节点: %w", err)
	}
	defer rows.Close()
	return scanFilesPG(rows)
}

func (s *PostgresStore) GetNamespaceUsage(ctx context.Context, namespace string) (*domain.NamespaceUsage, error) {
	usage := &domain.NamespaceUsage{}
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(size), 0) FROM files WHERE namespace = $1 AND is_dir = FALSE AND deleted_at IS NULL`, namespace).Scan(&usage.FileCount, &usage.TotalSize)
	if err != nil {
		return nil, err
	}
	return usage, nil
}

func (s *PostgresStore) ListTrash(ctx context.Context, namespace string) ([]*domain.FileMetadata, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT file_id, sha256, name, path, size, namespace, is_dir, parent_id, created_at, deleted_at FROM files WHERE namespace = $1 AND deleted_at IS NOT NULL ORDER BY deleted_at DESC, name ASC`, namespace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var files []*domain.FileMetadata
	for rows.Next() {
		var f domain.FileMetadata
		var sha, parent sql.NullString
		var created time.Time
		var deleted sql.NullTime
		if err := rows.Scan(&f.FileID, &sha, &f.Name, &f.Path, &f.Size, &f.Namespace, &f.IsDir, &parent, &created, &deleted); err != nil {
			return nil, err
		}
		f.SHA256, f.ParentID, f.CreatedAt = sha.String, parent.String, created
		if deleted.Valid {
			f.DeletedAt = &deleted.Time
		}
		files = append(files, &f)
	}
	return files, rows.Err()
}

func (s *PostgresStore) MoveFileToTrash(ctx context.Context, id string, deletedAt time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE files SET deleted_at = $1 WHERE file_id = $2 AND deleted_at IS NULL`, deletedAt.UTC(), id)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *PostgresStore) RestoreFile(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE files SET deleted_at = NULL WHERE file_id = $1 AND deleted_at IS NOT NULL`, id)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *PostgresStore) DeleteFile(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM files WHERE file_id = $1`, id)
	return err
}

func (s *PostgresStore) ListFilesByBlob(ctx context.Context, sha256 string) ([]*domain.FileMetadata, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT file_id, sha256, name, path, size, namespace, is_dir, parent_id, created_at FROM files WHERE sha256 = $1`, sha256)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFilesPG(rows)
}

func (s *PostgresStore) ListRootPage(ctx context.Context, namespace string, search, fileType string, page, perPage int, sortBy, sortOrder string) ([]*domain.FileMetadata, int, error) {
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
	countSQL := "SELECT COUNT(*) FROM files WHERE (parent_id IS NULL OR parent_id='') AND namespace=$1 AND deleted_at IS NULL"
	countArgs := []any{namespace}
	if search != "" {
		countSQL += " AND name ILIKE $2"
		countArgs = append(countArgs, "%"+search+"%")
	}
	countSQL += postgresFileTypeClause(fileType)
	if err := s.db.QueryRowContext(ctx, countSQL, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}
	sql := "SELECT file_id, sha256, name, path, size, namespace, is_dir, parent_id, created_at FROM files WHERE (parent_id IS NULL OR parent_id='') AND namespace=$1 AND deleted_at IS NULL"
	queryArgs := []any{namespace}
	paramIdx := 2
	if search != "" {
		sql += fmt.Sprintf(" AND name ILIKE $%d", paramIdx)
		queryArgs = append(queryArgs, "%"+search+"%")
		paramIdx++
	}
	sql += postgresFileTypeClause(fileType)
	sql += fmt.Sprintf(" ORDER BY %s %s, file_id ASC LIMIT $%d OFFSET $%d", safeSort, order, paramIdx, paramIdx+1)
	queryArgs = append(queryArgs, perPage, offset)
	rows, err := s.db.QueryContext(ctx, sql, queryArgs...)
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

func (s *PostgresStore) ListRoot(ctx context.Context, namespace string, search string) ([]*domain.FileMetadata, error) {
	var rows *sql.Rows
	var err error
	if search != "" {
		rows, err = s.db.QueryContext(ctx,
			`SELECT file_id, sha256, name, path, size, namespace, is_dir, parent_id, created_at FROM files WHERE namespace = $1 AND parent_id IS NULL AND deleted_at IS NULL AND name ILIKE $2 ORDER BY name`,
			namespace, "%"+search+"%")
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT file_id, sha256, name, path, size, namespace, is_dir, parent_id, created_at FROM files WHERE namespace = $1 AND parent_id IS NULL AND deleted_at IS NULL ORDER BY name`,
			namespace)
	}
	if err != nil {
		return nil, fmt.Errorf("列根目录: %w", err)
	}
	defer rows.Close()
	return scanFilesPG(rows)
}

// ========== 会话操作（PG 中复用关系表存储） ==========

func (s *PostgresStore) CreateSession(ctx context.Context, sess *domain.UploadSession) error {
	// 上传会话用 Redis 热数据，PG 不支持则跳过
	return nil
}
func (s *PostgresStore) GetSession(ctx context.Context, id string) (*domain.UploadSession, error) {
	return nil, nil
}
func (s *PostgresStore) UpdateOffset(ctx context.Context, id string, sliceIndex int, sliceSha string, addBytes int64) error {
	return nil
}
func (s *PostgresStore) ListChunks(ctx context.Context, id string) ([]domain.ChunkInfo, error) {
	return nil, nil
}
func (s *PostgresStore) DeleteSession(ctx context.Context, id string) error {
	return nil
}
func (s *PostgresStore) TouchSession(ctx context.Context, id string, ttl time.Duration) error {
	return nil
}
func (s *PostgresStore) ListExpiredSessions(ctx context.Context) ([]string, error) {
	return nil, nil
}

// ========== 标签管理 ==========

func (s *PostgresStore) RenameFile(ctx context.Context, fileID, newName, newPath string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE files SET name=$1, path=$2 WHERE file_id=$3", newName, newPath, fileID)
	return err
}

func (s *PostgresStore) SetFileTags(ctx context.Context, fileID string, tags []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM file_tags WHERE file_id = $1`, fileID); err != nil {
		return err
	}
	for _, tag := range tags {
		if tag == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO file_tags (file_id, tag, created_at) VALUES ($1, $2, NOW()) ON CONFLICT DO NOTHING`,
			fileID, tag); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *PostgresStore) GetFileTags(ctx context.Context, fileID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT tag FROM file_tags WHERE file_id = $1 ORDER BY tag`, fileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tags []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

func (s *PostgresStore) DeleteFileTags(ctx context.Context, fileID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM file_tags WHERE file_id = $1`, fileID)
	return err
}

// ========== 批量管理 ==========

func (s *PostgresStore) ReparentFile(ctx context.Context, fileID string, parentID *string, path string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE files SET parent_id = $1, path = $2 WHERE file_id = $3`,
		parentID, path, fileID)
	return err
}

func (s *PostgresStore) UpdateFileParent(ctx context.Context, fileID string, parentID *string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE files SET parent_id = $1 WHERE file_id = $2`,
		parentID, fileID)
	return err
}

// ========== 分享 ==========

func (s *PostgresStore) CreateShare(ctx context.Context, token string, entry *domain.ShareEntry) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO shares (token, file_id, password_hash, expires_at, max_downloads, cur_downloads, namespace, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())`,
		token, entry.FileID, entry.PasswordHash, entry.ExpiresAt, entry.MaxDownloads, entry.CurDownloads, entry.Namespace)
	return err
}

func (s *PostgresStore) GetShare(ctx context.Context, token string) (*domain.ShareEntry, error) {
	row := s.db.QueryRowContext(ctx, `SELECT token, file_id, password_hash, expires_at, max_downloads, cur_downloads, namespace, created_at FROM shares WHERE token = $1`, token)
	var e domain.ShareEntry
	var createdAt time.Time
	err := row.Scan(&e.Token, &e.FileID, &e.PasswordHash, &e.ExpiresAt, &e.MaxDownloads, &e.CurDownloads, &e.Namespace, &createdAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e.CreatedAt = createdAt.Format(time.RFC3339)
	return &e, nil
}

func (s *PostgresStore) ListShares(ctx context.Context, namespace, fileID string) ([]*domain.ShareEntry, error) {
	query := `SELECT token, file_id, password_hash, expires_at, max_downloads, cur_downloads, namespace, created_at FROM shares WHERE namespace = $1`
	args := []any{namespace}
	if fileID != "" {
		query += ` AND file_id = $2`
		args = append(args, fileID)
	}
	query += ` ORDER BY created_at DESC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := make([]*domain.ShareEntry, 0)
	for rows.Next() {
		var entry domain.ShareEntry
		var createdAt time.Time
		if err := rows.Scan(&entry.Token, &entry.FileID, &entry.PasswordHash, &entry.ExpiresAt, &entry.MaxDownloads, &entry.CurDownloads, &entry.Namespace, &createdAt); err != nil {
			return nil, err
		}
		entry.CreatedAt = createdAt.Format(time.RFC3339)
		entry.PasswordProtected = entry.PasswordHash != ""
		entries = append(entries, &entry)
	}
	return entries, rows.Err()
}

func (s *PostgresStore) DeleteShare(ctx context.Context, token, namespace string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM shares WHERE token = $1 AND namespace = $2`, token, namespace)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *PostgresStore) IncrDownloads(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE shares SET cur_downloads = cur_downloads + 1 WHERE token = $1`, token)
	return err
}

// ========== 一致性巡检 ==========

func (s *PostgresStore) ListAllBlobs(ctx context.Context) ([]*domain.ContentBlob, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT sha256, storage_path, size, ref_count, created_at FROM content_blobs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var blobs []*domain.ContentBlob
	for rows.Next() {
		var b domain.ContentBlob
		var createdAt time.Time
		if err := rows.Scan(&b.SHA256, &b.StoragePath, &b.Size, &b.RefCount, &createdAt); err != nil {
			return nil, err
		}
		b.CreatedAt = createdAt
		blobs = append(blobs, &b)
	}
	return blobs, rows.Err()
}

func (s *PostgresStore) ListAllFiles(ctx context.Context) ([]*domain.FileMetadata, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT file_id, sha256, name, path, size, namespace, is_dir, parent_id, created_at FROM files ORDER BY file_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFilesWithDeletedPG(rows)
}

// ========== 审计日志 ==========

func (s *PostgresStore) WriteAuditLog(ctx context.Context, entry *domain.AuditLogEntry) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_log (action, target_type, target_id, user_id, namespace, detail, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		entry.Action, entry.TargetType, entry.TargetID, entry.UserID, entry.Namespace, entry.Detail, time.Now())
	return err
}

func (s *PostgresStore) ListAuditLogs(ctx context.Context, page, perPage int) ([]*domain.AuditLogEntry, int, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 50
	}

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_log`).Scan(&total); err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * perPage
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, action, target_type, target_id, user_id, namespace, detail, created_at FROM audit_log ORDER BY id DESC LIMIT $1 OFFSET $2`,
		perPage, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var entries []*domain.AuditLogEntry
	for rows.Next() {
		var e domain.AuditLogEntry
		var createdAt time.Time
		if err := rows.Scan(&e.ID, &e.Action, &e.TargetType, &e.TargetID, &e.UserID, &e.Namespace, &e.Detail, &createdAt); err != nil {
			return nil, 0, err
		}
		e.CreatedAt = createdAt.Format(time.RFC3339)
		entries = append(entries, &e)
	}
	return entries, total, rows.Err()
}

func (s *PostgresStore) AdminCountFiles(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM files`).Scan(&count)
	return count, err
}

func (s *PostgresStore) AdminCountBlobs(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM content_blobs`).Scan(&count)
	return count, err
}

func (s *PostgresStore) AdminTotalBlobSize(ctx context.Context) (int64, error) {
	var total sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT SUM(size) FROM content_blobs`).Scan(&total)
	return total.Int64, err
}

func (s *PostgresStore) Close() error {
	return s.db.Close()
}

// ========== PostgreSQL 专用扫描函数 ==========

func scanBlob(row *sql.Row) (*domain.ContentBlob, error) {
	var b domain.ContentBlob
	var createdAt time.Time
	err := row.Scan(&b.SHA256, &b.StoragePath, &b.Size, &b.RefCount, &createdAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("扫描 blob: %w", err)
	}
	b.CreatedAt = createdAt
	return &b, nil
}

func scanFilePG(row *sql.Row) (*domain.FileMetadata, error) {
	var f domain.FileMetadata
	var sha256, parentID sql.NullString
	var createdAt time.Time
	err := row.Scan(&f.FileID, &sha256, &f.Name, &f.Path, &f.Size, &f.Namespace, &f.IsDir, &parentID, &createdAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("扫描文件: %w", err)
	}
	f.SHA256 = sha256.String
	f.ParentID = parentID.String
	f.CreatedAt = createdAt
	return &f, nil
}

func scanFilesPG(rows *sql.Rows) ([]*domain.FileMetadata, error) {
	var files []*domain.FileMetadata
	for rows.Next() {
		var f domain.FileMetadata
		var sha256, parentID sql.NullString
		var createdAt time.Time
		if err := rows.Scan(&f.FileID, &sha256, &f.Name, &f.Path, &f.Size, &f.Namespace, &f.IsDir, &parentID, &createdAt); err != nil {
			return nil, fmt.Errorf("扫描文件: %w", err)
		}
		f.SHA256 = sha256.String
		f.ParentID = parentID.String
		f.CreatedAt = createdAt
		files = append(files, &f)
	}
	return files, rows.Err()
}

func scanFilesWithDeletedPG(rows *sql.Rows) ([]*domain.FileMetadata, error) {
	var files []*domain.FileMetadata
	for rows.Next() {
		var f domain.FileMetadata
		var sha256, parentID sql.NullString
		var isDir int64
		var createdAt time.Time
		var deletedAt sql.NullTime
		if err := rows.Scan(&f.FileID, &sha256, &f.Name, &f.Path, &f.Size, &f.Namespace, &isDir, &parentID, &createdAt, &deletedAt); err != nil {
			return nil, fmt.Errorf("扫描文件: %w", err)
		}
		f.SHA256, f.ParentID, f.IsDir, f.CreatedAt = sha256.String, parentID.String, isDir != 0, createdAt
		if deletedAt.Valid {
			value := deletedAt.Time
			f.DeletedAt = &value
		}
		files = append(files, &f)
	}
	return files, rows.Err()
}

// compile-time assertion
var _ domain.Metadata = (*PostgresStore)(nil)

// HealthCheck 检查 Postgres 连接。
func (p *PostgresStore) HealthCheck(ctx context.Context) error {
	return p.db.PingContext(ctx)
}
