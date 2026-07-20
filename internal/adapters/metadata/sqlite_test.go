package metadata

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

func newTestSQLite(t *testing.T) *SQLiteStore {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	s, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore error = %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSQLiteStore_BlobCRUD(t *testing.T) {
	s := newTestSQLite(t)
	ctx := context.Background()

	// Put
	blob := &domain.ContentBlob{
		SHA256:      "abc123def456",
		StoragePath: "data/ns/file1",
		Size:        1024,
		RefCount:    1,
		CreatedAt:   time.Now(),
	}
	if err := s.PutBlob(ctx, blob); err != nil {
		t.Fatalf("PutBlob error = %v", err)
	}

	// Get
	got, err := s.GetBlobBySha(ctx, blob.SHA256)
	if err != nil {
		t.Fatalf("GetBlobBySha error = %v", err)
	}
	if got == nil {
		t.Fatal("GetBlobBySha returned nil")
	}
	if got.SHA256 != blob.SHA256 {
		t.Errorf("SHA256 = %s, want %s", got.SHA256, blob.SHA256)
	}
	if got.Size != blob.Size {
		t.Errorf("Size = %d, want %d", got.Size, blob.Size)
	}
	if got.RefCount != blob.RefCount {
		t.Errorf("RefCount = %d, want %d", got.RefCount, blob.RefCount)
	}

	// 不存在的 sha256
	notFound, err := s.GetBlobBySha(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetBlobBySha error = %v", err)
	}
	if notFound != nil {
		t.Error("不存在的 sha256 应返回 nil")
	}
}

func TestSQLiteStore_BatchLookups(t *testing.T) {
	s := newTestSQLite(t)
	ctx := context.Background()
	now := time.Now()

	for _, blob := range []*domain.ContentBlob{
		{SHA256: "batch-sha-1", StoragePath: "data/1", Size: 10, RefCount: 1, CreatedAt: now},
		{SHA256: "batch-sha-2", StoragePath: "data/2", Size: 20, RefCount: 2, CreatedAt: now},
	} {
		if err := s.PutBlob(ctx, blob); err != nil {
			t.Fatalf("PutBlob error = %v", err)
		}
	}
	blobs, err := s.GetBlobsBySha(ctx, []string{"batch-sha-2", "batch-sha-1", "batch-sha-2", "missing"})
	if err != nil {
		t.Fatalf("GetBlobsBySha error = %v", err)
	}
	if len(blobs) != 2 || blobs["batch-sha-2"].Size != 20 {
		t.Fatalf("GetBlobsBySha = %#v, want both batch blobs", blobs)
	}

	for _, file := range []*domain.FileMetadata{
		{FileID: "batch-file-1", SHA256: "batch-sha-1", Name: "one.txt", Path: "one.txt", Size: 10, Namespace: "ns", CreatedAt: now},
		{FileID: "batch-file-2", SHA256: "batch-sha-2", Name: "two.txt", Path: "two.txt", Size: 20, Namespace: "ns", CreatedAt: now},
	} {
		if err := s.PutFile(ctx, file); err != nil {
			t.Fatalf("PutFile error = %v", err)
		}
	}
	files, err := s.GetFilesByIDs(ctx, []string{"batch-file-2", "batch-file-1", "batch-file-2", "missing"})
	if err != nil {
		t.Fatalf("GetFilesByIDs error = %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("GetFilesByIDs count = %d, want 2", len(files))
	}
}

func TestSQLiteStore_BlobDuplicate(t *testing.T) {
	s := newTestSQLite(t)
	ctx := context.Background()

	blob := &domain.ContentBlob{
		SHA256:      "dup-sha",
		StoragePath: "data/ns/f1",
		Size:        100,
		RefCount:    1,
		CreatedAt:   time.Now(),
	}
	if err := s.PutBlob(ctx, blob); err != nil {
		t.Fatalf("PutBlob error = %v", err)
	}
	// 重复写入不应报错（INSERT OR IGNORE）
	if err := s.PutBlob(ctx, blob); err != nil {
		t.Fatalf("PutBlob duplicate error = %v", err)
	}
}

func TestSQLiteStore_RefCount(t *testing.T) {
	s := newTestSQLite(t)
	ctx := context.Background()

	blob := &domain.ContentBlob{
		SHA256:      "ref-test",
		StoragePath: "data/ns/f1",
		Size:        100,
		RefCount:    1,
		CreatedAt:   time.Now(),
	}
	s.PutBlob(ctx, blob)

	// Incr
	if err := s.IncrBlobRef(ctx, blob.SHA256); err != nil {
		t.Fatalf("IncrBlobRef error = %v", err)
	}

	got, _ := s.GetBlobBySha(ctx, blob.SHA256)
	if got.RefCount != 2 {
		t.Errorf("After Incr RefCount = %d, want 2", got.RefCount)
	}

	// Decr
	count, err := s.DecrBlobRef(ctx, blob.SHA256)
	if err != nil {
		t.Fatalf("DecrBlobRef error = %v", err)
	}
	if count != 1 {
		t.Errorf("After Decr RefCount = %d, want 1", count)
	}

	// Decr 到 0
	count, _ = s.DecrBlobRef(ctx, blob.SHA256)
	if count != 0 {
		t.Errorf("After second Decr RefCount = %d, want 0", count)
	}
}

func TestSQLiteStore_FileCRUD(t *testing.T) {
	s := newTestSQLite(t)
	ctx := context.Background()

	now := time.Now()
	file := &domain.FileMetadata{
		FileID:    "file-001",
		SHA256:    "sha-of-file",
		Name:      "test.txt",
		Path:      "test.txt",
		Size:      500,
		Namespace: "demo",
		IsDir:     false,
		CreatedAt: now,
	}

	if err := s.PutFile(ctx, file); err != nil {
		t.Fatalf("PutFile error = %v", err)
	}

	// GetFile
	got, err := s.GetFile(ctx, file.FileID)
	if err != nil {
		t.Fatalf("GetFile error = %v", err)
	}
	if got == nil {
		t.Fatal("GetFile returned nil")
	}
	if got.Name != file.Name {
		t.Errorf("Name = %s, want %s", got.Name, file.Name)
	}
	if got.Namespace != file.Namespace {
		t.Errorf("Namespace = %s, want %s", got.Namespace, file.Namespace)
	}
	if got.IsDir {
		t.Error("IsDir = true, want false")
	}

	// 不存在的 file id
	notFound, err := s.GetFile(ctx, "no-such")
	if err != nil {
		t.Fatalf("GetFile error = %v", err)
	}
	if notFound != nil {
		t.Error("不存在的 fileID 应返回 nil")
	}
}

func TestSQLiteStore_DirectoryTree(t *testing.T) {
	s := newTestSQLite(t)
	ctx := context.Background()

	now := time.Now()
	dir := &domain.FileMetadata{
		FileID:    "dir-001",
		Name:      "root",
		Path:      "/",
		Size:      0,
		Namespace: "demo",
		IsDir:     true,
		CreatedAt: now,
	}
	s.PutFile(ctx, dir)

	children := []*domain.FileMetadata{
		{FileID: "f1", Name: "a.txt", Path: "a.txt", Size: 100, Namespace: "demo", ParentID: "dir-001", CreatedAt: now},
		{FileID: "f2", Name: "b.txt", Path: "sub/b.txt", Size: 200, Namespace: "demo", ParentID: "dir-001", CreatedAt: now},
		{FileID: "f3", Name: "c.txt", Path: "sub/c.txt", Size: 300, Namespace: "demo", ParentID: "dir-001", CreatedAt: now},
	}
	for _, c := range children {
		s.PutFile(ctx, c)
	}

	// ListChildren
	listed, err := s.ListChildren(ctx, "dir-001", "")
	if err != nil {
		t.Fatalf("ListChildren error = %v", err)
	}
	if len(listed) != 3 {
		t.Fatalf("ListChildren count = %d, want 3", len(listed))
	}
	if listed[0].FileID != "f1" {
		t.Errorf("first child = %s, want f1", listed[0].FileID)
	}
}

func TestSQLiteStore_ListRoot(t *testing.T) {
	s := newTestSQLite(t)
	ctx := context.Background()

	now := time.Now()
	dirs := []*domain.FileMetadata{
		{FileID: "d1", Name: "dir1", Path: "/dir1", Namespace: "demo", IsDir: true, CreatedAt: now},
		{FileID: "d2", Name: "dir2", Path: "/dir2", Namespace: "other", IsDir: true, CreatedAt: now},
	}
	for _, d := range dirs {
		s.PutFile(ctx, d)
	}

	// 查询 demo namespace 的根节点
	roots, err := s.ListRoot(ctx, "demo", "")
	if err != nil {
		t.Fatalf("ListRoot error = %v", err)
	}
	if len(roots) != 1 {
		t.Fatalf("ListRoot count = %d, want 1", len(roots))
	}
	if roots[0].FileID != "d1" {
		t.Errorf("root = %s, want d1", roots[0].FileID)
	}
}

func TestSQLiteStore_ListPageFiltersByTypeAndReturnsExactTotal(t *testing.T) {
	s := newTestSQLite(t)
	ctx := context.Background()
	now := time.Now()

	files := []*domain.FileMetadata{
		{FileID: "root-dir", Name: "alpha-folder", Path: "/alpha-folder", Namespace: "demo", IsDir: true, CreatedAt: now},
		{FileID: "root-file-a", Name: "alpha.txt", Path: "alpha.txt", Namespace: "demo", CreatedAt: now},
		{FileID: "root-file-b", Name: "beta.txt", Path: "beta.txt", Namespace: "demo", CreatedAt: now},
		{FileID: "other-root", Name: "other.txt", Path: "other.txt", Namespace: "other", CreatedAt: now},
		{FileID: "child-dir", Name: "nested", Path: "alpha-folder/nested", Namespace: "demo", ParentID: "root-dir", IsDir: true, CreatedAt: now},
		{FileID: "child-file-a", Name: "archive.txt", Path: "alpha-folder/archive.txt", Namespace: "demo", ParentID: "root-dir", CreatedAt: now},
		{FileID: "child-file-b", Name: "report.pdf", Path: "alpha-folder/report.pdf", Namespace: "demo", ParentID: "root-dir", CreatedAt: now},
	}
	for _, file := range files {
		if err := s.PutFile(ctx, file); err != nil {
			t.Fatalf("PutFile(%s) error = %v", file.FileID, err)
		}
	}

	rootFiles, total, err := s.ListRootPage(ctx, "demo", "", "file", 2, 1, "name", "asc")
	if err != nil {
		t.Fatalf("ListRootPage(files) error = %v", err)
	}
	if total != 2 {
		t.Fatalf("ListRootPage(files) total = %d, want 2", total)
	}
	if len(rootFiles) != 1 || rootFiles[0].FileID != "root-file-b" || rootFiles[0].IsDir {
		t.Fatalf("ListRootPage(files) = %#v, want second file only", rootFiles)
	}

	rootDirs, total, err := s.ListRootPage(ctx, "demo", "", "dir", 1, 50, "name", "asc")
	if err != nil {
		t.Fatalf("ListRootPage(dirs) error = %v", err)
	}
	if total != 1 || len(rootDirs) != 1 || !rootDirs[0].IsDir || rootDirs[0].FileID != "root-dir" {
		t.Fatalf("ListRootPage(dirs) = %#v, total = %d; want root-dir and total 1", rootDirs, total)
	}

	childFiles, total, err := s.ListChildrenPage(ctx, "root-dir", "report", "file", 1, 50, "name", "asc")
	if err != nil {
		t.Fatalf("ListChildrenPage(files) error = %v", err)
	}
	if total != 1 || len(childFiles) != 1 || childFiles[0].FileID != "child-file-b" || childFiles[0].IsDir {
		t.Fatalf("ListChildrenPage(files) = %#v, total = %d; want report file and total 1", childFiles, total)
	}

	childDirs, total, err := s.ListChildrenPage(ctx, "root-dir", "", "dir", 1, 50, "name", "asc")
	if err != nil {
		t.Fatalf("ListChildrenPage(dirs) error = %v", err)
	}
	if total != 1 || len(childDirs) != 1 || !childDirs[0].IsDir || childDirs[0].FileID != "child-dir" {
		t.Fatalf("ListChildrenPage(dirs) = %#v, total = %d; want nested directory and total 1", childDirs, total)
	}
}

func TestSQLiteStore_DeleteFile(t *testing.T) {
	s := newTestSQLite(t)
	ctx := context.Background()

	file := &domain.FileMetadata{
		FileID:    "del-me",
		Name:      "delete.txt",
		Path:      "delete.txt",
		Size:      10,
		Namespace: "demo",
		CreatedAt: time.Now(),
	}
	s.PutFile(ctx, file)

	if err := s.DeleteFile(ctx, file.FileID); err != nil {
		t.Fatalf("DeleteFile error = %v", err)
	}

	got, _ := s.GetFile(ctx, file.FileID)
	if got != nil {
		t.Error("删除后 GetFile 应返回 nil")
	}
}

func TestSQLiteStore_ListFilesByBlob(t *testing.T) {
	s := newTestSQLite(t)
	ctx := context.Background()

	now := time.Now()
	sha := "shared-sha-256"
	files := []*domain.FileMetadata{
		{FileID: "f1", SHA256: sha, Name: "file1.txt", Path: "f1", Size: 100, Namespace: "ns", CreatedAt: now},
		{FileID: "f2", SHA256: sha, Name: "file2.txt", Path: "f2", Size: 200, Namespace: "ns", CreatedAt: now},
		{FileID: "f3", SHA256: "other-sha", Name: "f3.txt", Path: "f3", Size: 300, Namespace: "ns", CreatedAt: now},
	}
	for _, f := range files {
		s.PutFile(ctx, f)
	}

	refs, err := s.ListFilesByBlob(ctx, sha)
	if err != nil {
		t.Fatalf("ListFilesByBlob error = %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("引用数 = %d, want 2", len(refs))
	}
}

func TestSQLiteStore_GetFileByPath(t *testing.T) {
	s := newTestSQLite(t)
	ctx := context.Background()

	file := &domain.FileMetadata{
		FileID: "f1", Name: "doc.txt", Path: "docs/doc.txt", Size: 100,
		Namespace: "demo", CreatedAt: time.Now(),
	}
	s.PutFile(ctx, file)

	got, err := s.GetFileByPath(ctx, "demo", "docs/doc.txt")
	if err != nil {
		t.Fatalf("GetFileByPath error = %v", err)
	}
	if got == nil || got.FileID != "f1" {
		t.Errorf("GetFileByPath = %v, want f1", got)
	}

	// 不存在的路径
	notFound, _ := s.GetFileByPath(ctx, "demo", "nonexistent")
	if notFound != nil {
		t.Error("不存在的路径应返回 nil")
	}
}

func TestSQLiteStore_ListAll(t *testing.T) {
	s := newTestSQLite(t)
	ctx := context.Background()

	blob := &domain.ContentBlob{
		SHA256: "list-all-sha", StoragePath: "data/ns/f", Size: 100, RefCount: 1,
		CreatedAt: time.Now(),
	}
	s.PutBlob(ctx, blob)

	file := &domain.FileMetadata{
		FileID: "fa", Name: "a.txt", Path: "a", Size: 100,
		Namespace: "ns", CreatedAt: time.Now(),
	}
	s.PutFile(ctx, file)

	blobs, err := s.ListAllBlobs(ctx)
	if err != nil {
		t.Fatalf("ListAllBlobs error = %v", err)
	}
	if len(blobs) != 1 {
		t.Errorf("ListAllBlobs count = %d, want 1", len(blobs))
	}

	files, err := s.ListAllFiles(ctx)
	if err != nil {
		t.Fatalf("ListAllFiles error = %v", err)
	}
	if len(files) != 1 {
		t.Errorf("ListAllFiles count = %d, want 1", len(files))
	}
}

func TestSQLiteStore_FileTags(t *testing.T) {
	s := newTestSQLite(t)
	ctx := context.Background()

	// 先创建一个文件
	file := &domain.FileMetadata{
		FileID:    "tag-file-1",
		Name:      "test.txt",
		Path:      "test.txt",
		Size:      100,
		Namespace: "demo",
		CreatedAt: time.Now(),
	}
	if err := s.PutFile(ctx, file); err != nil {
		t.Fatalf("PutFile error = %v", err)
	}

	// 初始无标签
	tags, err := s.GetFileTags(ctx, file.FileID)
	if err != nil {
		t.Fatalf("GetFileTags error = %v", err)
	}
	if len(tags) != 0 {
		t.Fatalf("初始标签数 = %d, want 0", len(tags))
	}

	// 设置标签
	expected := []string{"important", "work"}
	if err := s.SetFileTags(ctx, file.FileID, expected); err != nil {
		t.Fatalf("SetFileTags error = %v", err)
	}

	// 读取标签
	tags, err = s.GetFileTags(ctx, file.FileID)
	if err != nil {
		t.Fatalf("GetFileTags error = %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("标签数 = %d, want 2", len(tags))
	}
	if tags[0] != "important" || tags[1] != "work" {
		t.Errorf("标签 = %v, want [important work]", tags)
	}

	// 覆盖标签
	updated := []string{"archive"}
	if err := s.SetFileTags(ctx, file.FileID, updated); err != nil {
		t.Fatalf("SetFileTags error = %v", err)
	}
	tags, _ = s.GetFileTags(ctx, file.FileID)
	if len(tags) != 1 || tags[0] != "archive" {
		t.Errorf("覆盖后标签 = %v, want [archive]", tags)
	}

	// 删除标签
	if err := s.DeleteFileTags(ctx, file.FileID); err != nil {
		t.Fatalf("DeleteFileTags error = %v", err)
	}
	tags, _ = s.GetFileTags(ctx, file.FileID)
	if len(tags) != 0 {
		t.Errorf("删除后标签数 = %d, want 0", len(tags))
	}
}

func TestSQLiteStore_Migrate_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "migrate.db")

	s1, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore error = %v", err)
	}
	s1.Close()

	// 再次打开同一数据库，迁移应幂等
	s2, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("第二次 NewSQLiteStore error = %v", err)
	}
	s2.Close()
}

func TestSQLiteStore_Migrate_RecordsVersionsAndUpgradesLegacySchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open error = %v", err)
	}
	legacy := []string{
		`CREATE TABLE content_blobs (sha256 TEXT PRIMARY KEY, storage_path TEXT NOT NULL, size BIGINT NOT NULL, ref_count INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL)`,
		`CREATE TABLE files (file_id TEXT PRIMARY KEY, sha256 TEXT, name TEXT NOT NULL, path TEXT NOT NULL, size BIGINT NOT NULL DEFAULT 0, namespace TEXT NOT NULL, is_dir INTEGER NOT NULL DEFAULT 0, parent_id TEXT, created_at TEXT NOT NULL)`,
	}
	for _, query := range legacy {
		if _, err := db.Exec(query); err != nil {
			_ = db.Close()
			t.Fatalf("create legacy schema: %v", err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("upgrade legacy schema: %v", err)
	}
	defer store.Close()

	var versions int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&versions); err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}
	if versions != len(sqliteMigrations) {
		t.Fatalf("migration count = %d, want %d", versions, len(sqliteMigrations))
	}
	var deletedAt int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('files') WHERE name = 'deleted_at'`).Scan(&deletedAt); err != nil {
		t.Fatalf("query deleted_at: %v", err)
	}
	if deletedAt != 1 {
		t.Fatalf("deleted_at columns = %d, want 1", deletedAt)
	}
	var auditIndexes int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name IN ('idx_audit_log_created_at', 'idx_audit_log_action_created')`).Scan(&auditIndexes); err != nil {
		t.Fatalf("query audit indexes: %v", err)
	}
	if auditIndexes != 2 {
		t.Fatalf("audit indexes = %d, want 2", auditIndexes)
	}
}

func TestSQLiteStore_Close(t *testing.T) {
	s := newTestSQLite(t)
	// Close 不应 panic
	if err := s.Close(); err != nil {
		t.Errorf("Close error = %v", err)
	}
}

func TestNullStr(t *testing.T) {
	if nullStr("") != nil {
		t.Error("空字符串 nullStr 应返回 nil")
	}
	if v := nullStr("hello"); v == nil || *v != "hello" {
		t.Error("非空字符串 nullStr 返回错误")
	}
}

func TestBoolToInt(t *testing.T) {
	if boolToInt(true) != 1 {
		t.Error("boolToInt(true) != 1")
	}
	if boolToInt(false) != 0 {
		t.Error("boolToInt(false) != 0")
	}
}

func TestSQLiteStore_AcquireBlobIncrementsExistingReference(t *testing.T) {
	s := newTestSQLite(t)
	ctx := context.Background()
	first := &domain.ContentBlob{SHA256: "acquire-sha", StoragePath: "data/first", Size: 10, RefCount: 1, CreatedAt: time.Now()}
	path, inserted, err := s.AcquireBlob(ctx, first)
	if err != nil || !inserted || path != first.StoragePath {
		t.Fatalf("first AcquireBlob = path %q inserted %v err %v", path, inserted, err)
	}
	second := &domain.ContentBlob{SHA256: first.SHA256, StoragePath: "data/second", Size: 10, RefCount: 1, CreatedAt: time.Now()}
	path, inserted, err = s.AcquireBlob(ctx, second)
	if err != nil || inserted || path != first.StoragePath {
		t.Fatalf("second AcquireBlob = path %q inserted %v err %v", path, inserted, err)
	}
	got, _ := s.GetBlobBySha(ctx, first.SHA256)
	if got.RefCount != 2 {
		t.Fatalf("RefCount = %d, want 2", got.RefCount)
	}
}

func TestSQLiteStore_TryConsumeDownloadHonorsLimit(t *testing.T) {
	s := newTestSQLite(t)
	ctx := context.Background()
	if err := s.CreateShare(ctx, "quota-share", &domain.ShareEntry{Token: "quota-share", FileID: "f", MaxDownloads: 1}); err != nil {
		t.Fatal(err)
	}
	ok, err := s.TryConsumeDownload(ctx, "quota-share")
	if err != nil || !ok {
		t.Fatalf("first consume = %v, %v", ok, err)
	}
	ok, err = s.TryConsumeDownload(ctx, "quota-share")
	if err != nil || ok {
		t.Fatalf("second consume = %v, %v", ok, err)
	}
}

func TestSQLiteStore_NamespaceQuotaReservations(t *testing.T) {
	s := newTestSQLite(t)
	ctx := context.Background()

	if err := s.ReserveNamespaceBytes(ctx, "demo", "upload-1", 8, 10); err != nil {
		t.Fatalf("first reservation error = %v", err)
	}
	if err := s.ReserveNamespaceBytes(ctx, "demo", "upload-2", 3, 10); err != domain.ErrQuotaExceeded {
		t.Fatalf("second reservation error = %v, want ErrQuotaExceeded", err)
	}
	if err := s.ReserveNamespaceBytes(ctx, "demo", "upload-1", 5, 10); err != nil {
		t.Fatalf("resize reservation error = %v", err)
	}
	if err := s.ReserveNamespaceBytes(ctx, "demo", "upload-2", 5, 10); err != nil {
		t.Fatalf("reservation after resize error = %v", err)
	}
	if err := s.ReleaseNamespaceReservation(ctx, "upload-1"); err != nil {
		t.Fatalf("release reservation error = %v", err)
	}
	if err := s.ReserveNamespaceBytes(ctx, "demo", "upload-3", 5, 10); err != nil {
		t.Fatalf("reservation after release error = %v", err)
	}
}
