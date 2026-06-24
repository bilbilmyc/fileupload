package domain

import (
	"bytes"
	"context"
	"testing"
	"time"
)

// T1：空目录删除应成功
func TestUploadService_DeleteDir_Empty(t *testing.T) {
	svc, meta, _ := newTestUploadService(t)
	ctx := context.Background()

	meta.PutFile(ctx, &FileMetadata{
		FileID: "empty-dir", Name: "empty", Namespace: "demo", IsDir: true, CreatedAt: time.Now(),
	})

	if err := svc.DeleteDir(ctx, "empty-dir", true, "demo"); err != nil {
		t.Fatalf("DeleteDir(empty) error = %v", err)
	}

	// 目录记录应已删除
	if f, _ := meta.GetFile(ctx, "empty-dir"); f != nil {
		t.Errorf("empty-dir 仍存在")
	}
}

// T2：含 2 个文件的目录删除 — 两个 blob 的 ref_count 都应 -1
func TestUploadService_DeleteDir_TwoFiles(t *testing.T) {
	svc, meta, _ := newTestUploadService(t)
	ctx := context.Background()

	meta.PutFile(ctx, &FileMetadata{
		FileID: "dir-t2", Name: "d", Namespace: "demo", IsDir: true, CreatedAt: time.Now(),
	})
	meta.PutFile(ctx, &FileMetadata{
		FileID: "f-t2-a", Name: "a.txt", Namespace: "demo", ParentID: "dir-t2", SHA256: "blobA", CreatedAt: time.Now(),
	})
	meta.PutFile(ctx, &FileMetadata{
		FileID: "f-t2-b", Name: "b.txt", Namespace: "demo", ParentID: "dir-t2", SHA256: "blobB", CreatedAt: time.Now(),
	})
	meta.PutBlob(ctx, &ContentBlob{SHA256: "blobA", StoragePath: "pA", RefCount: 2})
	meta.PutBlob(ctx, &ContentBlob{SHA256: "blobB", StoragePath: "pB", RefCount: 2})

	if err := svc.DeleteDir(ctx, "dir-t2", true, "demo"); err != nil {
		t.Fatalf("DeleteDir error = %v", err)
	}

	for _, sha := range []string{"blobA", "blobB"} {
		b, _ := meta.GetBlobBySha(ctx, sha)
		if b == nil {
			t.Fatalf("blob %s 丢失", sha)
		}
		if b.RefCount != 1 {
			t.Errorf("blob %s RefCount = %d, want 1 (原 2 - 1)", sha, b.RefCount)
		}
	}
}

// T3：嵌套目录递归 — 子目录及其所有 blob 都应清理
//
// 结构：
//   root/
//     sub/
//       deep.txt   (blobDeep)
//     top.txt      (blobTop)
func TestUploadService_DeleteDir_Nested(t *testing.T) {
	svc, meta, _ := newTestUploadService(t)
	ctx := context.Background()

	meta.PutFile(ctx, &FileMetadata{
		FileID: "root-t3", Name: "root", Namespace: "demo", IsDir: true, CreatedAt: time.Now(),
	})
	meta.PutFile(ctx, &FileMetadata{
		FileID: "sub-t3", Name: "sub", Namespace: "demo", ParentID: "root-t3", IsDir: true, CreatedAt: time.Now(),
	})
	meta.PutFile(ctx, &FileMetadata{
		FileID: "deep-t3", Name: "deep.txt", Namespace: "demo", ParentID: "sub-t3", SHA256: "blobDeep", CreatedAt: time.Now(),
	})
	meta.PutFile(ctx, &FileMetadata{
		FileID: "top-t3", Name: "top.txt", Namespace: "demo", ParentID: "root-t3", SHA256: "blobTop", CreatedAt: time.Now(),
	})
	meta.PutBlob(ctx, &ContentBlob{SHA256: "blobDeep", StoragePath: "pD", RefCount: 2})
	meta.PutBlob(ctx, &ContentBlob{SHA256: "blobTop", StoragePath: "pT", RefCount: 2})

	if err := svc.DeleteDir(ctx, "root-t3", true, "demo"); err != nil {
		t.Fatalf("DeleteDir(nested) error = %v", err)
	}

	// 所有文件 + 子目录记录都应消失
	for _, id := range []string{"root-t3", "sub-t3", "deep-t3", "top-t3"} {
		if f, _ := meta.GetFile(ctx, id); f != nil {
			t.Errorf("%s 仍存在", id)
		}
	}
	// 两个 blob ref_count 都 -1
	for _, sha := range []string{"blobDeep", "blobTop"} {
		b, _ := meta.GetBlobBySha(ctx, sha)
		if b == nil {
			t.Fatalf("blob %s 丢失", sha)
		}
		if b.RefCount != 1 {
			t.Errorf("blob %s RefCount = %d, want 1", sha, b.RefCount)
		}
	}
}

// T4：原子性 — deleteDir 中途失败应回滚所有已成功的操作
//
// 配置 mock 让第二次 DeleteFile 调用失败：
// - file1 删除 + decr 成功
// - file2 删除失败 → 应触发 file1 + blobA 的回滚
//
// 期望最终状态：所有文件记录仍在，blob ref_count 不变。
func TestUploadService_DeleteDir_Atomic(t *testing.T) {
	svc, meta, _ := newTestUploadService(t)
	ctx := context.Background()

	// 第二次 DeleteFile 调用失败（第一次成功，模拟中途失败）
	meta.SetFailAfterDeleteFile(1)

	meta.PutFile(ctx, &FileMetadata{
		FileID: "dir-t4", Name: "d", Namespace: "demo", IsDir: true, CreatedAt: time.Now(),
	})
	meta.PutFile(ctx, &FileMetadata{
		FileID: "f-t4-a", Name: "a.txt", Namespace: "demo", ParentID: "dir-t4", SHA256: "blobA", CreatedAt: time.Now(),
	})
	meta.PutFile(ctx, &FileMetadata{
		FileID: "f-t4-b", Name: "b.txt", Namespace: "demo", ParentID: "dir-t4", SHA256: "blobB", CreatedAt: time.Now(),
	})
	meta.PutBlob(ctx, &ContentBlob{SHA256: "blobA", StoragePath: "pA", RefCount: 2})
	meta.PutBlob(ctx, &ContentBlob{SHA256: "blobB", StoragePath: "pB", RefCount: 2})

	err := svc.DeleteDir(ctx, "dir-t4", true, "demo")
	if err == nil {
		t.Fatal("expected error from atomic delete")
	}
	t.Logf("got expected error: %v", err)

	// 验证：所有原状态保留
	for _, id := range []string{"dir-t4", "f-t4-a", "f-t4-b"} {
		if f, _ := meta.GetFile(ctx, id); f == nil {
			t.Errorf("%s 丢失（应被回滚）", id)
		}
	}
	for _, sha := range []string{"blobA", "blobB"} {
		b, _ := meta.GetBlobBySha(ctx, sha)
		if b == nil {
			t.Fatalf("blob %s 丢失", sha)
		}
		if b.RefCount != 2 {
			t.Errorf("blob %s RefCount = %d, want 2 (回滚)", sha, b.RefCount)
		}
	}
}

// T5：物理文件清理 — 当 blob 的 ref_count 因删除降到 0，物理文件应被 storage.Delete
//
// 场景：单个文件被删除，blob 原本只被这一个文件引用（RefCount=1）
// 期望：storage 中 "pA" 不再存在
func TestUploadService_DeleteDir_PhysicalCleanup(t *testing.T) {
	svc, meta, storage := newTestUploadService(t)
	ctx := context.Background()

	// 先把物理文件写入 mock storage
	storage.Write(ctx, "pA", bytes.NewReader([]byte("hello world")))

	meta.PutFile(ctx, &FileMetadata{
		FileID: "dir-t5", Name: "d", Namespace: "demo", IsDir: true, CreatedAt: time.Now(),
	})
	meta.PutFile(ctx, &FileMetadata{
		FileID: "f-t5-a", Name: "a.txt", Namespace: "demo", ParentID: "dir-t5", SHA256: "blobA", CreatedAt: time.Now(),
	})
	meta.PutBlob(ctx, &ContentBlob{SHA256: "blobA", StoragePath: "pA", RefCount: 1})

	if err := svc.DeleteDir(ctx, "dir-t5", true, "demo"); err != nil {
		t.Fatalf("DeleteDir error = %v", err)
	}

	// 物理文件应被删除
	if _, exists, _ := storage.Stat(ctx, "pA"); exists {
		t.Errorf("物理文件 pA 应已被删除")
	}
	// blob 引用计数应 = 0（mock 不删 blob 记录，只减计数）
	b, _ := meta.GetBlobBySha(ctx, "blobA")
	if b == nil {
		t.Fatal("blobA 丢失")
	}
	if b.RefCount != 0 {
		t.Errorf("blobA RefCount = %d, want 0", b.RefCount)
	}
}