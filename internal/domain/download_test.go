package domain

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"
)

func newTestDownloadService(t *testing.T) (*DownloadService, *mockMetadata, *mockStorage) {
	t.Helper()
	meta := newMockMetadata()
	storage := newMockStorage()
	compress := newMockCompressor()
	hasher := newMockHasher()

	cfg := DownloadConfig{DataDir: "data"}
	svc := NewDownloadService(meta, storage, compress, hasher, cfg)
	return svc, meta, storage
}

func setupTestFile(t *testing.T, meta *mockMetadata, storage *mockStorage, fileID, sha, ns, name string, size int64, data []byte) {
	t.Helper()
	blob := &ContentBlob{
		SHA256: sha, StoragePath: "" + ns + "/" + fileID,
		Size: size, RefCount: 1, CreatedAt: time.Now(),
	}
	meta.PutBlob(context.Background(), blob)

	file := &FileMetadata{
		FileID: fileID, SHA256: sha, Name: name,
		Path: name, Size: size, Namespace: ns, CreatedAt: time.Now(),
	}
	meta.PutFile(context.Background(), file)
	storage.Write(context.Background(), ns+"/"+fileID, bytes.NewReader(data))
}

func TestGetFile_Full(t *testing.T) {
	svc, meta, storage := newTestDownloadService(t)
	ctx := context.Background()

	data := []byte("full file download test data")
	setupTestFile(t, meta, storage, "f1", sha256Hex(data), "demo", "test.txt", int64(len(data)), data)

	fr, err := svc.GetFile(ctx, "f1", "demo", DownloadRange{})
	if err != nil {
		t.Fatalf("GetFile error = %v", err)
	}
	defer fr.Reader.Close()

	got, _ := io.ReadAll(fr.Reader)
	if !bytes.Equal(got, data) {
		t.Errorf("GetFile content mismatch")
	}
	if fr.FileSize != int64(len(data)) {
		t.Errorf("FileSize = %d, want %d", fr.FileSize, len(data))
	}
}

func TestGetFile_Range(t *testing.T) {
	svc, meta, storage := newTestDownloadService(t)
	ctx := context.Background()

	data := []byte("0123456789abcdef")
	setupTestFile(t, meta, storage, "range-file", sha256Hex(data), "ns", "range.bin", int64(len(data)), data)

	tests := []struct {
		name   string
		offset int64
		length int64
		want   string
	}{
		{"前 5 字节", 0, 5, "01234"},
		{"中间", 4, 4, "4567"},
		{"到末尾", 10, 0, "abcdef"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rng := DownloadRange{Offset: tt.offset, Length: tt.length}
			fr, err := svc.GetFile(ctx, "range-file", "ns", rng)
			if err != nil {
				t.Fatalf("GetFile error = %v", err)
			}
			defer fr.Reader.Close()

			got, _ := io.ReadAll(fr.Reader)
			if string(got) != tt.want {
				t.Errorf("GetFile(range) = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestGetFile_NotFound(t *testing.T) {
	svc, _, _ := newTestDownloadService(t)
	ctx := context.Background()

	_, err := svc.GetFile(ctx, "no-such", "ns", DownloadRange{})
	if err != ErrNotFound {
		t.Errorf("GetFile(no such) err = %v, want ErrNotFound", err)
	}
}

func TestGetFile_Forbidden(t *testing.T) {
	svc, meta, storage := newTestDownloadService(t)
	ctx := context.Background()

	data := []byte("secret")
	setupTestFile(t, meta, storage, "secret-file", sha256Hex(data), "ns1", "s.txt", int64(len(data)), data)

	_, err := svc.GetFile(ctx, "secret-file", "ns2", DownloadRange{})
	if err != ErrForbidden {
		t.Errorf("GetFile(wrong ns) err = %v, want ErrForbidden", err)
	}
}

func TestGetFile_Corrupted(t *testing.T) {
	svc, meta, _ := newTestDownloadService(t)
	ctx := context.Background()

	// 文件有记录但 blob 不存在
	file := &FileMetadata{
		FileID: "orphan", SHA256: "orphan-sha", Name: "orphan.txt",
		Path: "orphan", Size: 10, Namespace: "ns", CreatedAt: time.Now(),
	}
	meta.PutFile(ctx, file)

	_, err := svc.GetFile(ctx, "orphan", "ns", DownloadRange{})
	if err != ErrCorrupted {
		t.Errorf("GetFile(no blob) err = %v, want ErrCorrupted", err)
	}
}

func TestGetDirManifest(t *testing.T) {
	svc, meta, storage := newTestDownloadService(t)
	ctx := context.Background()

	now := time.Now()
	dir := &FileMetadata{
		FileID: "dir-1", Name: "root", Path: "/",
		Namespace: "demo", IsDir: true, CreatedAt: now,
	}
	meta.PutFile(ctx, dir)

	data1 := []byte("hello")
	data2 := []byte("world")
	setupTestFile(t, meta, storage, "f1", sha256Hex(data1), "demo", "a.txt", int64(len(data1)), data1)
	setupTestFile(t, meta, storage, "f2", sha256Hex(data2), "demo", "b.txt", int64(len(data2)), data2)

	// 设为子节点
	f1, _ := meta.GetFile(ctx, "f1")
	f1.ParentID = "dir-1"
	f2, _ := meta.GetFile(ctx, "f2")
	f2.ParentID = "dir-1"

	dw, err := svc.GetDirManifest(ctx, "dir-1", "demo")
	if err != nil {
		t.Fatalf("GetDirManifest error = %v", err)
	}
	if dw == nil {
		t.Fatal("GetDirManifest returned nil")
	}
	if len(dw.Entries) != 2 {
		t.Errorf("entries count = %d, want 2", len(dw.Entries))
	}
	if dw.TreeSHA256 == "" {
		t.Error("TreeSHA256 is empty")
	}
}

func TestGetDirManifest_NotDir(t *testing.T) {
	svc, meta, storage := newTestDownloadService(t)
	ctx := context.Background()

	data := []byte("file not dir")
	setupTestFile(t, meta, storage, "f1", sha256Hex(data), "ns", "f.txt", int64(len(data)), data)

	_, err := svc.GetDirManifest(ctx, "f1", "ns")
	if err != ErrInvalidArgument {
		t.Errorf("GetDirManifest(file) err = %v, want ErrInvalidArgument", err)
	}
}

func TestGetDirManifest_NotFound(t *testing.T) {
	svc, _, _ := newTestDownloadService(t)
	ctx := context.Background()

	_, err := svc.GetDirManifest(ctx, "no-such", "ns")
	if err != ErrNotFound {
		t.Errorf("GetDirManifest(no such) err = %v, want ErrNotFound", err)
	}
}

func TestStreamDir(t *testing.T) {
	svc, meta, storage := newTestDownloadService(t)
	ctx := context.Background()

	// 设置目录
	now := time.Now()
	dir := &FileMetadata{
		FileID: "stream-dir", Name: "root", Path: "/",
		Namespace: "demo", IsDir: true, CreatedAt: now,
	}
	meta.PutFile(ctx, dir)

	data1 := []byte("content1")
	data2 := []byte("content2")
	setupTestFile(t, meta, storage, "sf1", sha256Hex(data1), "demo", "a.txt", int64(len(data1)), data1)
	setupTestFile(t, meta, storage, "sf2", sha256Hex(data2), "demo", "b.txt", int64(len(data2)), data2)

	f1, _ := meta.GetFile(ctx, "sf1")
	f1.ParentID = "stream-dir"
	f2, _ := meta.GetFile(ctx, "sf2")
	f2.ParentID = "stream-dir"

	dw, _ := svc.GetDirManifest(ctx, "stream-dir", "demo")
	reader, err := svc.StreamDir(ctx, dw, CompTarGz)
	if err != nil {
		t.Fatalf("StreamDir error = %v", err)
	}
	defer reader.Close()

	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll StreamDir error = %v", err)
	}
	if len(output) == 0 {
		t.Error("StreamDir output is empty")
	}
}

func TestListDir_Root(t *testing.T) {
	svc, meta, _ := newTestDownloadService(t)
	ctx := context.Background()

	now := time.Now()
	meta.PutFile(ctx, &FileMetadata{
		FileID: "root-f1", Name: "a.txt", Path: "a.txt",
		Size: 100, Namespace: "demo", CreatedAt: now,
	})
	meta.PutFile(ctx, &FileMetadata{
		FileID: "root-f2", Name: "b.txt", Path: "b.txt",
		Size: 200, Namespace: "demo", CreatedAt: now,
	})

	dir, children, err := svc.ListDir(ctx, "/", "demo", "")
	if err != nil {
		t.Fatalf("ListDir(root) error = %v", err)
	}
	if dir != nil {
		t.Errorf("root dir should be nil")
	}
	if len(children) != 2 {
		t.Errorf("children count = %d, want 2", len(children))
	}
}

func TestListDir_Parent(t *testing.T) {
	svc, meta, _ := newTestDownloadService(t)
	ctx := context.Background()

	now := time.Now()
	dir := &FileMetadata{FileID: "mydir", Name: "mydir", Path: "/", Namespace: "demo", IsDir: true, CreatedAt: now}
	meta.PutFile(ctx, dir)

	meta.PutFile(ctx, &FileMetadata{FileID: "c1", Name: "c1.txt", Path: "c1", Size: 10, Namespace: "demo", ParentID: "mydir", CreatedAt: now})
	meta.PutFile(ctx, &FileMetadata{FileID: "c2", Name: "c2.txt", Path: "c2", Size: 20, Namespace: "demo", ParentID: "mydir", CreatedAt: now})

	parent, children, err := svc.ListDir(ctx, "mydir", "demo", "")
	if err != nil {
		t.Fatalf("ListDir error = %v", err)
	}
	if parent == nil || parent.FileID != "mydir" {
		t.Errorf("parent = %v", parent)
	}
	if len(children) != 2 {
		t.Errorf("children = %d, want 2", len(children))
	}
}

func TestListDir_NotFound(t *testing.T) {
	svc, _, _ := newTestDownloadService(t)
	ctx := context.Background()

	_, _, err := svc.ListDir(ctx, "no-such", "ns", "")
	if err != ErrNotFound {
		t.Errorf("ListDir(no such) err = %v, want ErrNotFound", err)
	}
}

func TestStat(t *testing.T) {
	svc, meta, storage := newTestDownloadService(t)
	ctx := context.Background()

	data := []byte("stat test data")
	setupTestFile(t, meta, storage, "stat-file", sha256Hex(data), "ns", "stat.txt", int64(len(data)), data)

	file, blob, err := svc.Stat(ctx, "stat-file", "ns")
	if err != nil {
		t.Fatalf("Stat error = %v", err)
	}
	if file == nil {
		t.Fatal("Stat file is nil")
	}
	if blob == nil {
		t.Fatal("Stat blob is nil")
	}
	if file.Name != "stat.txt" {
		t.Errorf("Name = %s", file.Name)
	}
	if blob.Size != int64(len(data)) {
		t.Errorf("blob size = %d", blob.Size)
	}
}

func TestStat_NotFound(t *testing.T) {
	svc, _, _ := newTestDownloadService(t)
	ctx := context.Background()

	_, _, err := svc.Stat(ctx, "no-such", "ns")
	if err != ErrNotFound {
		t.Errorf("Stat(no such) err = %v, want ErrNotFound", err)
	}
}

func TestStat_WrongNS(t *testing.T) {
	svc, meta, storage := newTestDownloadService(t)
	ctx := context.Background()

	data := []byte("private data")
	setupTestFile(t, meta, storage, "private", sha256Hex(data), "ns-a", "p.txt", int64(len(data)), data)

	_, _, err := svc.Stat(ctx, "private", "ns-b")
	if err != ErrNotFound {
		t.Errorf("Stat(wrong ns) err = %v, want ErrNotFound", err)
	}
}

func TestComputeTreeSHA256(t *testing.T) {
	entries := []DirEntryInfo{
		{Path: "a.txt", Size: 10, SHA256: "abc"},
		{Path: "b.txt", Size: 20, SHA256: "def"},
	}

	hash1 := computeTreeSHA256(entries)

	// 相同输入应产生相同哈希
	hash2 := computeTreeSHA256(entries)
	if hash1 != hash2 {
		t.Error("相同输入的 TreeSHA256 不一致")
	}

	// 不同输入应产生不同哈希
	entries2 := []DirEntryInfo{
		{Path: "a.txt", Size: 10, SHA256: "abc"},
		{Path: "c.txt", Size: 30, SHA256: "ghi"},
	}
	hash3 := computeTreeSHA256(entries2)
	if hash1 == hash3 {
		t.Error("不同输入的 TreeSHA256 应不同")
	}
}
