package metadata

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const postgresMigrationLockID int64 = 0x46494c4555504c44 // "FILEUPLD"

type postgresMigration struct {
	version int
	name    string
	queries []string
}

var postgresMigrations = []postgresMigration{
	{version: 1, name: "initial_schema", queries: []string{
		`CREATE TABLE IF NOT EXISTS content_blobs (
			sha256 TEXT PRIMARY KEY, storage_path TEXT NOT NULL, size BIGINT NOT NULL,
			ref_count INTEGER NOT NULL DEFAULT 0, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS files (
			file_id TEXT PRIMARY KEY, sha256 TEXT REFERENCES content_blobs(sha256), name TEXT NOT NULL,
			path TEXT NOT NULL DEFAULT '', size BIGINT NOT NULL DEFAULT 0, namespace TEXT NOT NULL,
			is_dir BOOLEAN NOT NULL DEFAULT FALSE, parent_id TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), deleted_at TIMESTAMPTZ
		)`,
		`CREATE INDEX IF NOT EXISTS idx_files_namespace_parent ON files(namespace, parent_id)`,
		`CREATE INDEX IF NOT EXISTS idx_files_sha256 ON files(sha256)`,
		`CREATE INDEX IF NOT EXISTS idx_files_path ON files(namespace, path)`,
		`CREATE TABLE IF NOT EXISTS file_tags (
			file_id TEXT NOT NULL REFERENCES files(file_id) ON DELETE CASCADE,
			tag TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), PRIMARY KEY (file_id, tag)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_file_tags_tag ON file_tags(tag)`,
		`CREATE TABLE IF NOT EXISTS audit_log (
			id BIGSERIAL PRIMARY KEY, action TEXT NOT NULL, target_type TEXT NOT NULL DEFAULT '',
			target_id TEXT NOT NULL DEFAULT '', user_id TEXT NOT NULL DEFAULT '',
			namespace TEXT NOT NULL DEFAULT '', detail TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS namespace_quota_locks (namespace TEXT PRIMARY KEY)`,
		`CREATE TABLE IF NOT EXISTS namespace_reservations (
			reservation_id TEXT PRIMARY KEY, namespace TEXT NOT NULL, bytes BIGINT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_namespace_reservations_namespace ON namespace_reservations(namespace)`,
		`CREATE TABLE IF NOT EXISTS shares (
			token TEXT PRIMARY KEY, file_id TEXT NOT NULL, password_hash TEXT NOT NULL DEFAULT '',
			expires_at TEXT NOT NULL DEFAULT '', max_downloads INTEGER NOT NULL DEFAULT 0,
			cur_downloads INTEGER NOT NULL DEFAULT 0, namespace TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
	}},
	{version: 2, name: "trash_deleted_at", queries: []string{
		`ALTER TABLE files ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ`,
		`CREATE INDEX IF NOT EXISTS idx_files_namespace_deleted ON files(namespace, deleted_at)`,
	}},
	{version: 3, name: "audit_indexes", queries: []string{
		`CREATE INDEX IF NOT EXISTS idx_audit_log_created_at ON audit_log(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_log_action_created ON audit_log(action, created_at)`,
	}},
}

func runPostgresMigrations(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("获取迁移连接: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, postgresMigrationLockID); err != nil {
		return fmt.Errorf("获取迁移锁: %w", err)
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, postgresMigrationLockID)
	}()

	if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`); err != nil {
		return fmt.Errorf("创建 schema_migrations: %w", err)
	}

	for _, migration := range postgresMigrations {
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("开始迁移 v%d: %w", migration.version, err)
		}
		var applied bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)`, migration.version).Scan(&applied); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("检查迁移 v%d: %w", migration.version, err)
		}
		if applied {
			_ = tx.Rollback()
			continue
		}
		for _, query := range migration.queries {
			if _, err := tx.ExecContext(ctx, query); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("执行迁移 v%d (%s): %w", migration.version, migration.name, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version, name) VALUES ($1, $2)`, migration.version, migration.name); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("记录迁移 v%d: %w", migration.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("提交迁移 v%d: %w", migration.version, err)
		}
	}
	return nil
}
