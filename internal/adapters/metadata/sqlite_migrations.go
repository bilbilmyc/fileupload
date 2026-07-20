package metadata

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type sqliteMigration struct {
	version int
	name    string
	apply   func(*sql.Tx) error
}

var sqliteMigrations = []sqliteMigration{
	{version: 1, name: "initial_schema", apply: func(tx *sql.Tx) error {
		return execSQLiteMigration(tx, []string{
			`CREATE TABLE IF NOT EXISTS content_blobs (
				sha256 TEXT PRIMARY KEY, storage_path TEXT NOT NULL, size BIGINT NOT NULL,
				ref_count INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL DEFAULT (datetime('now'))
			)`,
			`CREATE TABLE IF NOT EXISTS files (
				file_id TEXT PRIMARY KEY, sha256 TEXT REFERENCES content_blobs(sha256), name TEXT NOT NULL,
				path TEXT NOT NULL, size BIGINT NOT NULL DEFAULT 0, namespace TEXT NOT NULL,
				is_dir INTEGER NOT NULL DEFAULT 0, parent_id TEXT,
				created_at TEXT NOT NULL DEFAULT (datetime('now')), deleted_at TEXT
			)`,
			`CREATE INDEX IF NOT EXISTS idx_files_namespace_parent ON files(namespace, parent_id)`,
			`CREATE INDEX IF NOT EXISTS idx_files_sha256 ON files(sha256)`,
			`CREATE INDEX IF NOT EXISTS idx_files_path ON files(namespace, path)`,
			`CREATE TABLE IF NOT EXISTS file_tags (
				file_id TEXT NOT NULL REFERENCES files(file_id) ON DELETE CASCADE,
				tag TEXT NOT NULL, created_at TEXT NOT NULL DEFAULT (datetime('now')),
				PRIMARY KEY (file_id, tag)
			)`,
			`CREATE INDEX IF NOT EXISTS idx_file_tags_tag ON file_tags(tag)`,
			auditLogMigration,
			`CREATE TABLE IF NOT EXISTS namespace_quota_locks (namespace TEXT PRIMARY KEY)`,
			`CREATE TABLE IF NOT EXISTS namespace_reservations (
				reservation_id TEXT PRIMARY KEY, namespace TEXT NOT NULL, bytes BIGINT NOT NULL,
				created_at TEXT NOT NULL DEFAULT (datetime('now'))
			)`,
			`CREATE INDEX IF NOT EXISTS idx_namespace_reservations_namespace ON namespace_reservations(namespace)`,
			`CREATE TABLE IF NOT EXISTS shares (
				token TEXT PRIMARY KEY, file_id TEXT NOT NULL, password_hash TEXT NOT NULL DEFAULT '',
				expires_at TEXT NOT NULL DEFAULT '', max_downloads INTEGER NOT NULL DEFAULT 0,
				cur_downloads INTEGER NOT NULL DEFAULT 0, namespace TEXT NOT NULL DEFAULT '',
				created_at TEXT NOT NULL DEFAULT (datetime('now'))
			)`,
		})
	}},
	{version: 2, name: "trash_deleted_at", apply: func(tx *sql.Tx) error {
		var count int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('files') WHERE name = 'deleted_at'`).Scan(&count); err != nil {
			return fmt.Errorf("检查 files.deleted_at: %w", err)
		}
		if count == 0 {
			if _, err := tx.Exec(`ALTER TABLE files ADD COLUMN deleted_at TEXT`); err != nil {
				return fmt.Errorf("添加 files.deleted_at: %w", err)
			}
		}
		_, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_files_namespace_deleted ON files(namespace, deleted_at)`)
		return err
	}},
	{version: 3, name: "audit_indexes", apply: func(tx *sql.Tx) error {
		return execSQLiteMigration(tx, []string{
			`CREATE INDEX IF NOT EXISTS idx_audit_log_created_at ON audit_log(created_at)`,
			`CREATE INDEX IF NOT EXISTS idx_audit_log_action_created ON audit_log(action, created_at)`,
		})
	}},
}

func runSQLiteMigrations(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("创建 schema_migrations: %w", err)
	}

	for _, migration := range sqliteMigrations {
		tx, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			return fmt.Errorf("开始迁移 v%d: %w", migration.version, err)
		}
		var applied int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, migration.version).Scan(&applied); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("检查迁移 v%d: %w", migration.version, err)
		}
		if applied != 0 {
			_ = tx.Rollback()
			continue
		}
		if err := migration.apply(tx); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("执行迁移 v%d (%s): %w", migration.version, migration.name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version, name, applied_at) VALUES (?, ?, ?)`,
			migration.version, migration.name, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("记录迁移 v%d: %w", migration.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("提交迁移 v%d: %w", migration.version, err)
		}
	}
	return nil
}

func execSQLiteMigration(tx *sql.Tx, queries []string) error {
	for _, query := range queries {
		if _, err := tx.Exec(query); err != nil {
			return err
		}
	}
	return nil
}
