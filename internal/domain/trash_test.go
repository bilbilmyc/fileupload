package domain

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestTrashRestoreAndPurgeDirectory(t *testing.T) {
	svc, meta, storage := newTestUploadService(t)
	ctx := context.Background()

	meta.PutBlob(ctx, &ContentBlob{SHA256: "trash-blob", StoragePath: "demo/child", Size: 4, RefCount: 1, CreatedAt: time.Now()})
	if _, err := storage.Write(ctx, "demo/child", bytes.NewReader([]byte("data"))); err != nil {
		t.Fatal(err)
	}
	meta.PutFile(ctx, &FileMetadata{FileID: "trash-dir", Name: "archive", Path: "/", Namespace: "demo", IsDir: true, CreatedAt: time.Now()})
	meta.PutFile(ctx, &FileMetadata{FileID: "trash-child", SHA256: "trash-blob", Name: "notes.txt", Path: "archive/notes.txt", Namespace: "demo", ParentID: "trash-dir", Size: 4, CreatedAt: time.Now()})

	if err := svc.Trash(ctx, "trash-dir", "demo"); err != nil {
		t.Fatalf("Trash: %v", err)
	}
	if file, _ := meta.GetFile(ctx, "trash-dir"); file != nil {
		t.Fatal("trashed directory must not be visible in normal listings")
	}
	if files, err := meta.ListTrash(ctx, "demo"); err != nil || len(files) != 2 {
		t.Fatalf("ListTrash = %d, %v; want 2, nil", len(files), err)
	}
	blob, _ := meta.GetBlobBySha(ctx, "trash-blob")
	if blob.RefCount != 1 {
		t.Fatalf("trash must retain blob reference, got %d", blob.RefCount)
	}

	if err := svc.RestoreFromTrash(ctx, "trash-dir", "demo"); err != nil {
		t.Fatalf("RestoreFromTrash: %v", err)
	}
	if file, _ := meta.GetFile(ctx, "trash-child"); file == nil {
		t.Fatal("restored child should be visible")
	}

	if err := svc.Trash(ctx, "trash-dir", "demo"); err != nil {
		t.Fatalf("Trash second time: %v", err)
	}
	if err := svc.PurgeFromTrash(ctx, "trash-dir", "demo"); err != nil {
		t.Fatalf("PurgeFromTrash: %v", err)
	}
	if meta.hasFile("trash-dir") || meta.hasFile("trash-child") {
		t.Fatal("purge must delete directory metadata and descendants")
	}
	blob, _ = meta.GetBlobBySha(ctx, "trash-blob")
	if blob.RefCount != 0 {
		t.Fatalf("purge must decrement blob reference, got %d", blob.RefCount)
	}
	if storage.has("demo/child") {
		t.Fatal("purge must delete unreferenced physical content")
	}
}
