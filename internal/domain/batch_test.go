package domain

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"
)

func setupBatchTest(t *testing.T) (*BatchService, *mockMetadata) {
	t.Helper()
	meta := newMockMetadata()

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

	// 创建 UploadService 作为 FileDeleter / FileMover 的实现
	storage := newMockStorage()
	storage.Write(ctx, "p1", bytes.NewReader(make([]byte, 100)))
	storage.Write(ctx, "p2", bytes.NewReader(make([]byte, 200)))

	compress := newMockCompressor()
	hasher := newMockHasher()
	wp := newMockWorkerPool()

	uploadSvc := NewUploadService(meta, storage, storage, compress, hasher, wp, UploadConfig{
		DataDir: "data", SessionTTL: time.Minute, DefaultChunkSize: 1024,
	})
	downloadSvc := NewDownloadService(meta, storage, compress, hasher, DownloadConfig{DataDir: "data"})

	return NewBatchService(uploadSvc, uploadSvc, downloadSvc, meta), meta
}

var ctx = context.Background()

func TestBatchService_BatchDelete(t *testing.T) {
	svc, _ := setupBatchTest(t)
	result, err := svc.BatchDelete(ctx, []string{"f1", "f2"}, "demo")
	if err != nil {
		t.Fatalf("BatchDelete error = %v", err)
	}
	if result.Success != 2 {
		t.Errorf("Success = %d, want 2", result.Success)
	}
}

func TestBatchService_BatchDelete_NotFound(t *testing.T) {
	svc, _ := setupBatchTest(t)
	result, err := svc.BatchDelete(ctx, []string{"no-such"}, "demo")
	if err != nil {
		t.Fatalf("BatchDelete error = %v", err)
	}
	if result.Failed != 1 {
		t.Errorf("Failed = %d, want 1", result.Failed)
	}
}

func TestBatchService_BatchMove(t *testing.T) {
	svc, meta := setupBatchTest(t)
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
	svc, meta := setupBatchTest(t)
	now := time.Now()
	meta.PutFile(ctx, &FileMetadata{
		FileID: "target-dir", Name: "target", Namespace: "demo", IsDir: true, CreatedAt: now,
	})

	err := svc.BatchCopy(ctx, []string{"f1"}, "target-dir", "demo")
	if err != nil {
		t.Fatalf("BatchCopy error = %v", err)
	}
}

// TestBatchService_BatchCopy_IncrementsRefCount 回归测试：BatchCopy 应增加 blob 引用计数。
// 修复前此测试会失败（候选 #1 bug：引用计数被静默吞掉）。
// setupBatchTest 把 blob1/2 的 RefCount 都设成 1；复制一次后应变成 2。
func TestBatchService_BatchCopy_IncrementsRefCount(t *testing.T) {
	svc, meta := setupBatchTest(t)
	now := time.Now()
	meta.PutFile(ctx, &FileMetadata{
		FileID: "target-dir", Name: "target", Namespace: "demo", IsDir: true, CreatedAt: now,
	})

	if err := svc.BatchCopy(ctx, []string{"f1", "f2"}, "target-dir", "demo"); err != nil {
		t.Fatalf("BatchCopy error = %v", err)
	}

	for _, sha := range []string{"blob1", "blob2"} {
		b, _ := meta.GetBlobBySha(ctx, sha)
		if b == nil {
			t.Fatalf("blob %s not found", sha)
		}
		if b.RefCount != 2 {
			t.Errorf("blob %s RefCount = %d, want 2 (原始 1 + 复制 1)", sha, b.RefCount)
		}
	}
}

// TestBatchService_BatchCopyDir_IncrementsRefCount 回归测试：递归目录复制也应增加引用计数。
// setupBatchTest 中 dir-1 包含 f1/blob1 和 f2/blob2；复制整个目录后引用计数应各 +1。
func TestBatchService_BatchCopyDir_IncrementsRefCount(t *testing.T) {
	svc, meta := setupBatchTest(t)
	now := time.Now()
	meta.PutFile(ctx, &FileMetadata{
		FileID: "target-dir", Name: "target", Namespace: "demo", IsDir: true, CreatedAt: now,
	})

	if err := svc.BatchCopy(ctx, []string{"dir-1"}, "target-dir", "demo"); err != nil {
		t.Fatalf("BatchCopy(dir) error = %v", err)
	}

	for _, sha := range []string{"blob1", "blob2"} {
		b, _ := meta.GetBlobBySha(ctx, sha)
		if b == nil {
			t.Fatalf("blob %s not found", sha)
		}
		if b.RefCount != 2 {
			t.Errorf("blob %s RefCount = %d, want 2 (目录递归复制后)", sha, b.RefCount)
		}
	}
}

func TestBatchService_BatchTag(t *testing.T) {
	svc, _ := setupBatchTest(t)
	err := svc.BatchTag(ctx, []string{"f1", "f2"}, []string{"important"}, "demo")
	if err != nil {
		t.Fatalf("BatchTag error = %v", err)
	}
}

func TestBatchService_BatchDownload(t *testing.T) {
	svc, _ := setupBatchTest(t)
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
	svc, _ := setupBatchTest(t)
	_, err := svc.BatchDownload(ctx, []string{}, "demo", CompZip)
	if err == nil {
		t.Fatal("expected error for empty IDs")
	}
}

func TestBatchService_BatchDelete_EmptyIDs(t *testing.T) {
	svc, _ := setupBatchTest(t)
	result, err := svc.BatchDelete(ctx, []string{}, "demo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success != 0 || result.Failed != 0 {
		t.Errorf("expected empty result, got %+v", result)
	}
}

func TestBatchService_BatchMove_EmptyIDs(t *testing.T) {
	svc, _ := setupBatchTest(t)
	err := svc.BatchMove(ctx, []string{}, "target", "demo")
	if err == nil {
		t.Fatal("expected error for empty IDs")
	}
}

func TestBatchService_BatchCopy_EmptyIDs(t *testing.T) {
	svc, _ := setupBatchTest(t)
	err := svc.BatchCopy(ctx, []string{}, "target", "demo")
	if err == nil {
		t.Fatal("expected error for empty IDs")
	}
}

func TestBatchService_BatchTag_EmptyIDs(t *testing.T) {
	svc, _ := setupBatchTest(t)
	err := svc.BatchTag(ctx, []string{}, []string{"tag"}, "demo")
	if err != nil {
		t.Fatalf("expected no error for empty IDs, got: %v", err)
	}
}
