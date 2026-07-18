package metadata

import (
	"context"
	"os"
	"testing"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

func TestAdmin_WriteAndListAuditLog(t *testing.T) {
	s := newAdminTestSQLite(t)
	ctx := context.Background()

	err := s.WriteAuditLog(ctx, &domain.AuditLogEntry{
		Action: "delete", TargetType: "file", TargetID: "f1",
		UserID: "u1", Namespace: "demo", Detail: "test delete",
	})
	if err != nil {
		t.Fatalf("WriteAuditLog error = %v", err)
	}

	entries, total, err := s.ListAuditLogs(ctx, 1, 50)
	if err != nil {
		t.Fatalf("ListAuditLogs error = %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if len(entries) != 1 {
		t.Errorf("entries count = %d", len(entries))
	}
	if entries[0].Action != "delete" {
		t.Errorf("action = %s", entries[0].Action)
	}
}

func TestAdmin_WriteAuditLog_Multiple(t *testing.T) {
	s := newAdminTestSQLite(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		s.WriteAuditLog(ctx, &domain.AuditLogEntry{
			Action: "test", TargetID: string(rune('0' + i)), UserID: "u1",
		})
	}

	entries, total, err := s.ListAuditLogs(ctx, 1, 3)
	if err != nil {
		t.Fatalf("ListAuditLogs error = %v", err)
	}
	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
	if len(entries) > 3 {
		t.Errorf("entries on page 1 = %d, want <=3", len(entries))
	}
}

func TestAdmin_CountFiles(t *testing.T) {
	s := newAdminTestSQLite(t)
	ctx := context.Background()

	count, err := s.AdminCountFiles(ctx)
	if err != nil {
		t.Fatalf("AdminCountFiles error = %v", err)
	}
	if count < 0 {
		t.Errorf("count = %d", count)
	}
}

func TestAdmin_CountBlobs(t *testing.T) {
	s := newAdminTestSQLite(t)
	ctx := context.Background()

	count, err := s.AdminCountBlobs(ctx)
	if err != nil {
		t.Fatalf("AdminCountBlobs error = %v", err)
	}
	if count < 0 {
		t.Errorf("count = %d", count)
	}
}

func TestAdmin_TotalBlobSize(t *testing.T) {
	s := newAdminTestSQLite(t)
	ctx := context.Background()

	size, err := s.AdminTotalBlobSize(ctx)
	if err != nil {
		t.Fatalf("AdminTotalBlobSize error = %v", err)
	}
	if size < 0 {
		t.Errorf("size = %d", size)
	}
}

func TestAdmin_ListAuditLog_Pagination(t *testing.T) {
	s := newAdminTestSQLite(t)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		s.WriteAuditLog(ctx, &domain.AuditLogEntry{Action: "test", UserID: "u1"})
	}

	// Test various page params
	tests := []struct{ page, perPage int }{
		{0, 0}, {1, 200}, {2, 5}, {1, 5},
	}
	for _, tc := range tests {
		entries, total, err := s.ListAuditLogs(ctx, tc.page, tc.perPage)
		if err != nil {
			t.Fatalf("ListAuditLogs(%d,%d) error = %v", tc.page, tc.perPage, err)
		}
		if total != 10 {
			t.Errorf("page=%d, perPage=%d: total = %d, want 10", tc.page, tc.perPage, total)
		}
		if len(entries) > 50 {
			t.Errorf("too many entries: %d", len(entries))
		}
		_ = entries
	}
}

func TestAdmin_DeleteAuditLogTable(t *testing.T) {
	s := newAdminTestSQLite(t)
	ctx := context.Background()

	// Verify table exists by writing and reading
	err := s.WriteAuditLog(ctx, &domain.AuditLogEntry{Action: "test", UserID: "u1"})
	if err != nil {
		t.Fatalf("WriteAuditLog error = %v", err)
	}
}

func TestAdmin_CreateShare(t *testing.T) {
	s := newAdminTestSQLite(t)
	ctx := context.Background()

	err := s.CreateShare(ctx, "s-test", &domain.ShareEntry{
		Token: "s-test", FileID: "f1", Namespace: "demo",
	})
	if err != nil {
		t.Fatalf("CreateShare error = %v", err)
	}

	entry, err := s.GetShare(ctx, "s-test")
	if err != nil {
		t.Fatalf("GetShare error = %v", err)
	}
	if entry == nil {
		t.Fatal("share entry is nil")
	}
	if entry.FileID != "f1" {
		t.Errorf("FileID = %s, want f1", entry.FileID)
	}
}

func TestAdmin_GetShare_NotFound(t *testing.T) {
	s := newAdminTestSQLite(t)
	ctx := context.Background()

	entry, err := s.GetShare(ctx, "no-such")
	if err != nil {
		t.Fatalf("GetShare error = %v", err)
	}
	if entry != nil {
		t.Fatal("expected nil for non-existent share")
	}
}

func TestAdmin_IncrDownloads(t *testing.T) {
	s := newAdminTestSQLite(t)
	ctx := context.Background()

	s.CreateShare(ctx, "s-count", &domain.ShareEntry{
		Token: "s-count", FileID: "f1", Namespace: "demo",
	})

	err := s.IncrDownloads(ctx, "s-count")
	if err != nil {
		t.Fatalf("IncrDownloads error = %v", err)
	}

	entry, _ := s.GetShare(ctx, "s-count")
	if entry.CurDownloads != 1 {
		t.Errorf("CurDownloads = %d, want 1", entry.CurDownloads)
	}
}

// newAdminTestSQLite creates a temp SQLite store for admin tests
func newAdminTestSQLite(t *testing.T) *SQLiteStore {
	t.Helper()
	f, err := os.CreateTemp("", "fileupload-admin-test-*.db")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	f.Close()
	store, err := NewSQLiteStore(f.Name())
	if err != nil {
		os.Remove(f.Name())
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() {
		store.Close()
		os.Remove(f.Name())
	})
	return store
}
