package metadata

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/bilbilmyc/fileupload/internal/domain"
)

func newTestFacade(t *testing.T) *Facade {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	hot := NewRedisStore(client, "test:")

	path := t.TempDir() + "/facade.db"
	cold, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore error = %v", err)
	}

	f := NewFacade(hot, cold)
	t.Cleanup(func() { f.Close() })
	return f
}

func TestFacadeSessionLifecycle(t *testing.T) {
	f := newTestFacade(t)
	ctx := context.Background()

	now := time.Now()
	s := &domain.UploadSession{
		SessionID: "facade-sess", SHA256: "facade-sha",
		UploadLength: 1000, Compression: domain.CompZstd,
		ChunkSize: 256, Namespace: "demo", FileName: "f.bin",
		CreatedAt: now, ExpireAt: now.Add(time.Hour),
		Status: domain.SessionActive,
	}

	// Create + Get
	if err := f.CreateSession(ctx, s); err != nil {
		t.Fatalf("CreateSession error = %v", err)
	}
	got, err := f.GetSession(ctx, "facade-sess")
	if err != nil {
		t.Fatalf("GetSession error = %v", err)
	}
	if got == nil || got.SessionID != "facade-sess" {
		t.Fatal("GetSession failed")
	}

	// UpdateOffset + ListChunks
	if err := f.UpdateOffset(ctx, "facade-sess", 0, "sha-0", 128); err != nil {
		t.Fatalf("UpdateOffset error = %v", err)
	}
	chunks, err := f.ListChunks(ctx, "facade-sess")
	if err != nil {
		t.Fatalf("ListChunks error = %v", err)
	}
	if len(chunks) != 1 {
		t.Errorf("chunks = %d, want 1", len(chunks))
	}

	// TouchSession
	if err := f.TouchSession(ctx, "facade-sess", time.Hour); err != nil {
		t.Fatalf("TouchSession error = %v", err)
	}

	// ListExpiredSessions
	expired, err := f.ListExpiredSessions(ctx)
	if err != nil {
		t.Fatalf("ListExpiredSessions error = %v", err)
	}
	_ = expired

	// DeleteSession
	if err := f.DeleteSession(ctx, "facade-sess"); err != nil {
		t.Fatalf("DeleteSession error = %v", err)
	}
}

func TestFacadeBlobFileCRUD(t *testing.T) {
	f := newTestFacade(t)
	ctx := context.Background()

	// Blob
	blob := &domain.ContentBlob{
		SHA256: "facade-blob", StoragePath: "data/ns/f",
		Size: 500, RefCount: 1, CreatedAt: time.Now(),
	}
	if err := f.PutBlob(ctx, blob); err != nil {
		t.Fatalf("PutBlob error = %v", err)
	}
	gotBlob, err := f.GetBlobBySha(ctx, "facade-blob")
	if err != nil || gotBlob == nil {
		t.Fatal("GetBlobBySha failed")
	}
	if gotBlob.Size != 500 {
		t.Errorf("Size = %d, want 500", gotBlob.Size)
	}

	// Incr/Decr
	if err := f.IncrBlobRef(ctx, "facade-blob"); err != nil {
		t.Fatalf("IncrBlobRef error = %v", err)
	}
	count, err := f.DecrBlobRef(ctx, "facade-blob")
	if err != nil || count != 1 {
		t.Errorf("DecrBlobRef count = %d, want 1", count)
	}

	// File
	now := time.Now()
	file := &domain.FileMetadata{
		FileID: "facade-f1", SHA256: "facade-blob",
		Name: "f1.txt", Path: "f1", Size: 500,
		Namespace: "demo", CreatedAt: now,
	}
	if err := f.PutFile(ctx, file); err != nil {
		t.Fatalf("PutFile error = %v", err)
	}
	gotFile, _ := f.GetFile(ctx, "facade-f1")
	if gotFile == nil || gotFile.Name != "f1.txt" {
		t.Fatal("GetFile failed")
	}

	// ByPath
	byPath, _ := f.GetFileByPath(ctx, "demo", "f1")
	if byPath == nil {
		t.Fatal("GetFileByPath failed")
	}

	// Dir tree
	dir := &domain.FileMetadata{
		FileID: "facade-dir", Name: "root", Path: "/",
		Namespace: "demo", IsDir: true, CreatedAt: now,
	}
	f.PutFile(ctx, dir)

	// 创建带 parentID 的子文件
	child := &domain.FileMetadata{
		FileID: "facade-child", SHA256: "facade-blob",
		Name: "child.txt", Path: "child", Size: 100,
		Namespace: "demo", ParentID: "facade-dir", CreatedAt: now,
	}
	f.PutFile(ctx, child)

	children, _ := f.ListChildren(ctx, "facade-dir")
	if len(children) != 1 {
		t.Errorf("children = %d, want 1", len(children))
	}

	// ListRoot（facade-f1 和 facade-dir 都是根节点）
	roots, _ := f.ListRoot(ctx, "demo")
	if len(roots) != 2 {
		t.Errorf("roots = %d, want 2 (facade-f1 + facade-dir)", len(roots))
	}

	// ListFilesByBlob（facade-f1 和 facade-child 都引用 facade-blob）
	refs, _ := f.ListFilesByBlob(ctx, "facade-blob")
	if len(refs) != 2 {
		t.Errorf("refs = %d, want 2", len(refs))
	}

	// Delete
	if err := f.DeleteFile(ctx, "facade-f1"); err != nil {
		t.Fatalf("DeleteFile error = %v", err)
	}
	deletedFile, _ := f.GetFile(ctx, "facade-f1")
	if deletedFile != nil {
		t.Error("DeleteFile failed")
	}

	// ListAll
	allBlobs, _ := f.ListAllBlobs(ctx)
	if len(allBlobs) < 1 {
		t.Error("ListAllBlobs empty")
	}
	allFiles, _ := f.ListAllFiles(ctx)
	if len(allFiles) < 1 {
		t.Error("ListAllFiles empty")
	}
}
