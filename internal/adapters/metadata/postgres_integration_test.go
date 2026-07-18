package metadata

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

// 从环境变量读取 PG DSN，未设置时跳过测试。
// CI 通过 PostgreSQL service 注入该环境变量；本地可手动设置后运行。
func pgDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("FILEUPLOAD_PG_DSN")
	if dsn == "" {
		t.Skip("跳过 PostgreSQL 集成测试：未设置 FILEUPLOAD_PG_DSN")
	}
	return dsn
}

func newIntegrationPostgresStore(t *testing.T) *PostgresStore {
	t.Helper()
	store, err := NewPostgresStore(pgDSN(t))
	if err != nil {
		t.Fatalf("NewPostgresStore error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func uniquePGID(prefix string) string {
	return prefix + time.Now().Format("20060102150405.000000000")
}

func TestPostgresStore_ConnectAndMigrate(t *testing.T) {
	_ = newIntegrationPostgresStore(t)
}

func TestPostgresStore_PutAndGetBlob(t *testing.T) {
	store := newIntegrationPostgresStore(t)
	ctx := context.Background()
	blob := &domain.ContentBlob{
		SHA256:      uniquePGID("test-sha-"),
		StoragePath: "pg-test/data",
		Size:        100,
		RefCount:    1,
		CreatedAt:   time.Now(),
	}

	if err := store.PutBlob(ctx, blob); err != nil {
		t.Fatalf("PutBlob error = %v", err)
	}

	got, err := store.GetBlobBySha(ctx, blob.SHA256)
	if err != nil {
		t.Fatalf("GetBlobBySha error = %v", err)
	}
	if got == nil {
		t.Fatal("GetBlobBySha returned nil")
	}
	if got.Size != 100 {
		t.Errorf("Size = %d, want 100", got.Size)
	}
}

func TestPostgresStore_IncrDecrBlobRef(t *testing.T) {
	store := newIntegrationPostgresStore(t)
	ctx := context.Background()

	sha := uniquePGID("ref-test-")
	if err := store.PutBlob(ctx, &domain.ContentBlob{SHA256: sha, StoragePath: "p", Size: 10, RefCount: 1, CreatedAt: time.Now()}); err != nil {
		t.Fatalf("PutBlob error = %v", err)
	}

	if err := store.IncrBlobRef(ctx, sha); err != nil {
		t.Fatalf("IncrBlobRef error = %v", err)
	}

	count, err := store.DecrBlobRef(ctx, sha)
	if err != nil {
		t.Fatalf("DecrBlobRef error = %v", err)
	}
	if count != 1 {
		t.Errorf("ref count after decr = %d, want 1", count)
	}
}

func TestPostgresStore_AcquireBlob(t *testing.T) {
	store := newIntegrationPostgresStore(t)
	ctx := context.Background()
	sha := uniquePGID("acquire-test-")
	first := &domain.ContentBlob{SHA256: sha, StoragePath: "canonical/path", Size: 42, CreatedAt: time.Now()}

	path, inserted, err := store.AcquireBlob(ctx, first)
	if err != nil {
		t.Fatalf("first AcquireBlob error = %v", err)
	}
	if !inserted || path != first.StoragePath {
		t.Fatalf("first AcquireBlob = (%q, %v), want (%q, true)", path, inserted, first.StoragePath)
	}

	path, inserted, err = store.AcquireBlob(ctx, &domain.ContentBlob{SHA256: sha, StoragePath: "duplicate/path", Size: 42, CreatedAt: time.Now()})
	if err != nil {
		t.Fatalf("second AcquireBlob error = %v", err)
	}
	if inserted || path != first.StoragePath {
		t.Fatalf("second AcquireBlob = (%q, %v), want (%q, false)", path, inserted, first.StoragePath)
	}

	blob, err := store.GetBlobBySha(ctx, sha)
	if err != nil {
		t.Fatalf("GetBlobBySha error = %v", err)
	}
	if blob.RefCount != 2 {
		t.Errorf("RefCount = %d, want 2", blob.RefCount)
	}
}

func TestPostgresStore_NamespaceQuotaReservations(t *testing.T) {
	store := newIntegrationPostgresStore(t)
	ctx := context.Background()
	ns := uniquePGID("pg-quota-")

	if err := store.ReserveNamespaceBytes(ctx, ns, "r1-"+ns, 8, 10); err != nil {
		t.Fatalf("first ReserveNamespaceBytes error = %v", err)
	}
	if err := store.ReserveNamespaceBytes(ctx, ns, "r2-"+ns, 3, 10); err != domain.ErrQuotaExceeded {
		t.Fatalf("second ReserveNamespaceBytes error = %v, want %v", err, domain.ErrQuotaExceeded)
	}
	if err := store.ReserveNamespaceBytes(ctx, ns, "r1-"+ns, 5, 10); err != nil {
		t.Fatalf("updating reservation error = %v", err)
	}
	if err := store.ReserveNamespaceBytes(ctx, ns, "r2-"+ns, 5, 10); err != nil {
		t.Fatalf("reservation after update error = %v", err)
	}
	if err := store.ReleaseNamespaceReservation(ctx, "r1-"+ns); err != nil {
		t.Fatalf("ReleaseNamespaceReservation error = %v", err)
	}
	if err := store.ReserveNamespaceBytes(ctx, ns, "r3-"+ns, 6, 10); err != nil {
		t.Fatalf("reservation after release error = %v", err)
	}
}

func TestPostgresStore_PutAndGetFile(t *testing.T) {
	store := newIntegrationPostgresStore(t)
	ctx := context.Background()

	sha := uniquePGID("file-test-")
	if err := store.PutBlob(ctx, &domain.ContentBlob{SHA256: sha, StoragePath: "p", Size: 50, RefCount: 1, CreatedAt: time.Now()}); err != nil {
		t.Fatalf("PutBlob error = %v", err)
	}

	file := &domain.FileMetadata{
		FileID:    uniquePGID("pg-file-"),
		SHA256:    sha,
		Name:      "test.txt",
		Path:      "/test.txt",
		Size:      50,
		Namespace: "pg-test",
		CreatedAt: time.Now(),
	}

	if err := store.PutFile(ctx, file); err != nil {
		t.Fatalf("PutFile error = %v", err)
	}

	got, err := store.GetFile(ctx, file.FileID)
	if err != nil {
		t.Fatalf("GetFile error = %v", err)
	}
	if got == nil {
		t.Fatal("GetFile returned nil")
	}
	if got.Name != "test.txt" {
		t.Errorf("Name = %s", got.Name)
	}
}

func TestPostgresStore_ListRoot(t *testing.T) {
	store := newIntegrationPostgresStore(t)
	ctx := context.Background()

	ns := uniquePGID("pg-test-list-")
	for _, file := range []*domain.FileMetadata{
		{FileID: "r1-" + ns, Name: "a.txt", Namespace: ns, Size: 10, CreatedAt: time.Now()},
		{FileID: "r2-" + ns, Name: "b.txt", Namespace: ns, Size: 20, CreatedAt: time.Now()},
	} {
		if err := store.PutFile(ctx, file); err != nil {
			t.Fatalf("PutFile error = %v", err)
		}
	}

	children, err := store.ListRoot(ctx, ns, "")
	if err != nil {
		t.Fatalf("ListRoot error = %v", err)
	}
	if len(children) != 2 {
		t.Errorf("children = %d, want 2", len(children))
	}
}

func TestPostgresStore_ListWithSearch(t *testing.T) {
	store := newIntegrationPostgresStore(t)
	ctx := context.Background()

	parentID := uniquePGID("pg-search-parent-")
	ns := uniquePGID("pg-search-")
	for _, file := range []*domain.FileMetadata{
		{FileID: "s1-" + ns, Name: "alpha.txt", Namespace: ns, Size: 1, ParentID: parentID, CreatedAt: time.Now()},
		{FileID: "s2-" + ns, Name: "beta.txt", Namespace: ns, Size: 2, ParentID: parentID, CreatedAt: time.Now()},
	} {
		if err := store.PutFile(ctx, file); err != nil {
			t.Fatalf("PutFile error = %v", err)
		}
	}

	children, err := store.ListChildren(ctx, parentID, "alpha")
	if err != nil {
		t.Fatalf("ListChildren(search) error = %v", err)
	}
	if len(children) != 1 {
		t.Errorf("search results = %d, want 1", len(children))
	}
	if len(children) > 0 && children[0].Name != "alpha.txt" {
		t.Errorf("Name = %s, want alpha.txt", children[0].Name)
	}
}

func TestPostgresStore_Tags(t *testing.T) {
	store := newIntegrationPostgresStore(t)
	ctx := context.Background()

	fileID := uniquePGID("pg-tag-")
	if err := store.PutFile(ctx, &domain.FileMetadata{
		FileID: fileID, Name: "tag.txt", Namespace: "pg-test", Size: 1, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("PutFile error = %v", err)
	}

	if err := store.SetFileTags(ctx, fileID, []string{"important", "backup"}); err != nil {
		t.Fatalf("SetFileTags error = %v", err)
	}

	tags, err := store.GetFileTags(ctx, fileID)
	if err != nil {
		t.Fatalf("GetFileTags error = %v", err)
	}
	if len(tags) != 2 {
		t.Errorf("tags = %v, want 2", tags)
	}
}

func TestPostgresStore_AuditLog(t *testing.T) {
	store := newIntegrationPostgresStore(t)
	ctx := context.Background()

	if err := store.WriteAuditLog(ctx, &domain.AuditLogEntry{
		Action: "delete", TargetType: "file", UserID: "u1", Namespace: "pg-test", Detail: "test",
	}); err != nil {
		t.Fatalf("WriteAuditLog error = %v", err)
	}

	entries, total, err := store.ListAuditLogs(ctx, 1, 50)
	if err != nil {
		t.Fatalf("ListAuditLogs error = %v", err)
	}
	if total < 1 {
		t.Errorf("total = %d", total)
	}
	_ = entries
}

func TestPostgresStore_AdminCounts(t *testing.T) {
	store := newIntegrationPostgresStore(t)
	ctx := context.Background()

	count, err := store.AdminCountFiles(ctx)
	if err != nil {
		t.Fatalf("AdminCountFiles error = %v", err)
	}
	t.Logf("total files in PG: %d", count)
}
