package domain

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"
)

func setupBatchTest(t *testing.T) (*BatchService, *mockMetadata, *mockStorage) {
	t.Helper()
	meta := newMockMetadata()
	storage := newMockStorage()
	compress := newMockCompressor()
	hasher := newMockHasher()

	wp := newMockWorkerPool()
	uploadSvc := NewUploadService(meta, storage, storage, compress, hasher, wp, UploadConfig{
		DataDir: "data", SessionTTL: time.Minute, DefaultChunkSize: 1024,
	})
	downloadSvc := NewDownloadService(meta, storage, compress, hasher, DownloadConfig{DataDir: "data"})

	now := time.Now()
	meta.PutFile(ctx, &FileMetadata{
		FileID: "dir-1", Name: "mydir", Namespace: "demo", IsDir: true, CreatedAt: now,
	})
	meta.PutFile(ctx, &FileMetadata{
		FileID: "f1", Name: "a.txt", Namespace: "demo", Size: 100, ParentID: "dir-1", SHA256: "blob1", CreatedAt: now,
	})
	meta.PutFile(ctx, &FileMetadata{
		FileID: "f2", Name: "b.txt", Namespace: "demo", Size: 200, ParentID: "dir-1", SHA256: "blob2", CreatedAt: now,
	})
	meta.PutBlob(ctx, &ContentBlob{SHA256: "blob1", StoragePath: "p1", Size: 100, RefCount: 1})
	meta.PutBlob(ctx, &ContentBlob{SHA256: "blob2", StoragePath: "p2", Size: 200, RefCount: 1})
	storage.Write(ctx, "p1", bytes.NewReader(make([]byte, 100)))
	storage.Write(ctx, "p2", bytes.NewReader(make([]byte, 200)))

	return NewBatchService(uploadSvc, downloadSvc, meta, storage, compress), meta, storage
}

var ctx = context.Background()

func TestBatchService_BatchDelete(t *testing.T) {
	svc, _, _ := setupBatchTest(t)
	result, err := svc.BatchDelete(ctx, []string{"f1", "f2"}, "demo")
	if err != nil {
		t.Fatalf("BatchDelete error = %v", err)
	}
	if result.Success != 2 {
		t.Errorf("Success = %d, want 2", result.Success)
	}
}

func TestBatchService_BatchDelete_NotFound(t *testing.T) {
	svc, _, _ := setupBatchTest(t)
	result, err := svc.BatchDelete(ctx, []string{"no-such"}, "demo")
	if err != nil {
		t.Fatalf("BatchDelete error = %v", err)
	}
	if result.Failed != 1 {
		t.Errorf("Failed = %d, want 1", result.Failed)
	}
}

func TestBatchService_BatchMove(t *testing.T) {
	svc, meta, _ := setupBatchTest(t)
	now := time.Now()
	meta.PutFile(ctx, &FileMetadata{
		FileID: "target-dir", Name: "target", Namespace: "demo", IsDir: true, CreatedAt: now,
	})

	err := svc.BatchMove(ctx, []string{"f1"}, "target-dir", "demo")
	if err != nil {
		t.Fatalf("BatchMove error = %v", err)
	}

	file, _ := meta.GetFile(ctx, "f1")
	if file.ParentID != "target-dir" {
		t.Errorf("ParentID = %s, want target-dir", file.ParentID)
	}
}

func TestBatchService_BatchCopy(t *testing.T) {
	svc, meta, _ := setupBatchTest(t)
	now := time.Now()
	meta.PutFile(ctx, &FileMetadata{
		FileID: "target-dir", Name: "target", Namespace: "demo", IsDir: true, CreatedAt: now,
	})

	err := svc.BatchCopy(ctx, []string{"f1"}, "target-dir", "demo")
	if err != nil {
		t.Fatalf("BatchCopy error = %v", err)
	}
}

func TestBatchService_BatchTag(t *testing.T) {
	svc, _, _ := setupBatchTest(t)
	err := svc.BatchTag(ctx, []string{"f1", "f2"}, []string{"important"}, "demo")
	if err != nil {
		t.Fatalf("BatchTag error = %v", err)
	}
}

func TestBatchService_BatchDownload(t *testing.T) {
	svc, _, _ := setupBatchTest(t)
	reader, err := svc.BatchDownload(ctx, []string{"f1", "f2"}, "demo", CompZip)
	if err != nil {
		t.Fatalf("BatchDownload error = %v", err)
	}
	defer reader.Close()

	data, _ := io.ReadAll(reader)
	if len(data) == 0 {
		t.Error("BatchDownload output is empty")
	}
}

func TestBatchService_BatchDownload_EmptyIDs(t *testing.T) {
	svc, _, _ := setupBatchTest(t)
	_, err := svc.BatchDownload(ctx, []string{}, "demo", CompZip)
	if err == nil {
		t.Fatal("expected error for empty IDs")
	}
}

func TestBatchService_BatchDelete_EmptyIDs(t *testing.T) {
	svc, _, _ := setupBatchTest(t)
	result, err := svc.BatchDelete(ctx, []string{}, "demo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success != 0 || result.Failed != 0 {
		t.Errorf("expected empty result, got %+v", result)
	}
}

func TestBatchService_BatchMove_EmptyIDs(t *testing.T) {
	svc, _, _ := setupBatchTest(t)
	err := svc.BatchMove(ctx, []string{}, "target", "demo")
	if err == nil {
		t.Fatal("expected error for empty IDs")
	}
}

func TestBatchService_BatchCopy_EmptyIDs(t *testing.T) {
	svc, _, _ := setupBatchTest(t)
	err := svc.BatchCopy(ctx, []string{}, "target", "demo")
	if err == nil {
		t.Fatal("expected error for empty IDs")
	}
}

func TestBatchService_BatchTag_EmptyIDs(t *testing.T) {
	svc, _, _ := setupBatchTest(t)
	err := svc.BatchTag(ctx, []string{}, []string{"tag"}, "demo")
	if err != nil {
		t.Fatalf("expected no error for empty IDs, got: %v", err)
	}
}
