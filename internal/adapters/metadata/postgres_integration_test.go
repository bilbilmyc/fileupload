package metadata

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

// 从环境变量读取 PG DSN，未设置时跳过测试
func pgDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("FILEUPLOAD_PG_DSN")
	if dsn == "" {
		dsn = "postgres://postgres:Postgres%402026@12.2.40.40:35432/fileupload?sslmode=disable"
	}
	return dsn
}

func TestPostgresStore_ConnectAndMigrate(t *testing.T) {
	store, err := NewPostgresStore(pgDSN(t))
	if err != nil {
		t.Fatalf("NewPostgresStore error = %v", err)
	}
	defer store.Close()
}

func TestPostgresStore_PutAndGetBlob(t *testing.T) {
	store, err := NewPostgresStore(pgDSN(t))
	if err != nil {
		t.Fatalf("NewPostgresStore error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	blob := &domain.ContentBlob{
		SHA256: "test-sha-" + time.Now().Format("150405.000"),
		StoragePath: "pg-test/data",
		Size: 100,
		RefCount: 1,
		CreatedAt: time.Now(),
	}

	err = store.PutBlob(ctx, blob)
	if err != nil {
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
	store, _ := NewPostgresStore(pgDSN(t))
	defer store.Close()
	ctx := context.Background()

	sha := "ref-test-" + time.Now().Format("150405.000")
	store.PutBlob(ctx, &domain.ContentBlob{SHA256: sha, StoragePath: "p", Size: 10, RefCount: 1, CreatedAt: time.Now()})

	err := store.IncrBlobRef(ctx, sha)
	if err != nil {
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

func TestPostgresStore_PutAndGetFile(t *testing.T) {
	store, _ := NewPostgresStore(pgDSN(t))
	defer store.Close()
	ctx := context.Background()

	sha := "file-test-" + time.Now().Format("150405.000")
	store.PutBlob(ctx, &domain.ContentBlob{SHA256: sha, StoragePath: "p", Size: 50, RefCount: 1, CreatedAt: time.Now()})

	file := &domain.FileMetadata{
		FileID: "pg-file-" + time.Now().Format("150405.000"),
		SHA256: sha,
		Name:   "test.txt",
		Path:   "/test.txt",
		Size:   50,
		Namespace: "pg-test",
		CreatedAt: time.Now(),
	}

	err := store.PutFile(ctx, file)
	if err != nil {
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
	store, _ := NewPostgresStore(pgDSN(t))
	defer store.Close()
	ctx := context.Background()

	ns := "pg-test-list-" + time.Now().Format("150405")
	store.PutFile(ctx, &domain.FileMetadata{
		FileID: "r1-" + ns, Name: "a.txt", Namespace: ns, Size: 10, CreatedAt: time.Now(),
	})
	store.PutFile(ctx, &domain.FileMetadata{
		FileID: "r2-" + ns, Name: "b.txt", Namespace: ns, Size: 20, CreatedAt: time.Now(),
	})

	children, err := store.ListRoot(ctx, ns, "")
	if err != nil {
		t.Fatalf("ListRoot error = %v", err)
	}
	if len(children) != 2 {
		t.Errorf("children = %d, want 2", len(children))
	}
}

func TestPostgresStore_ListWithSearch(t *testing.T) {
	store, _ := NewPostgresStore(pgDSN(t))
	defer store.Close()
	ctx := context.Background()

	parentID := "pg-search-parent-" + time.Now().Format("150405.000")
	ns := "pg-search-" + time.Now().Format("150405")
	store.PutFile(ctx, &domain.FileMetadata{
		FileID: "s1-" + ns, Name: "alpha.txt", Namespace: ns, Size: 1, ParentID: parentID, CreatedAt: time.Now(),
	})
	store.PutFile(ctx, &domain.FileMetadata{
		FileID: "s2-" + ns, Name: "beta.txt", Namespace: ns, Size: 2, ParentID: parentID, CreatedAt: time.Now(),
	})

	// ILIKE 搜索（使用唯一 parentID 避免测试间干扰）
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
	store, _ := NewPostgresStore(pgDSN(t))
	defer store.Close()
	ctx := context.Background()

	fileID := "pg-tag-" + time.Now().Format("150405.000")
	store.PutFile(ctx, &domain.FileMetadata{
		FileID: fileID, Name: "tag.txt", Namespace: "pg-test", Size: 1, CreatedAt: time.Now(),
	})

	err := store.SetFileTags(ctx, fileID, []string{"important", "backup"})
	if err != nil {
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
	store, _ := NewPostgresStore(pgDSN(t))
	defer store.Close()
	ctx := context.Background()

	err := store.WriteAuditLog(ctx, &domain.AuditLogEntry{
		Action: "delete", TargetType: "file", UserID: "u1", Namespace: "pg-test", Detail: "test",
	})
	if err != nil {
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
	store, _ := NewPostgresStore(pgDSN(t))
	defer store.Close()
	ctx := context.Background()

	count, err := store.AdminCountFiles(ctx)
	if err != nil {
		t.Fatalf("AdminCountFiles error = %v", err)
	}
	t.Logf("total files in PG: %d", count)
}
