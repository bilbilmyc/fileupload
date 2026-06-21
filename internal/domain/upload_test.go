package domain

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func newTestUploadService(t *testing.T) (*UploadService, *mockMetadata, *mockStorage) {
	t.Helper()
	meta := newMockMetadata()
	storage := newMockStorage()
	compress := newMockCompressor()
	hasher := newMockHasher()
	pool := newMockWorkerPool()

	cfg := UploadConfig{
		SessionTTL:       time.Hour,
		DataDir:          "data",
		TempDir:          "tmp",
		DefaultChunkSize: 1024 * 1024,
	}

	svc := NewUploadService(meta, storage, storage, compress, hasher, pool, cfg)
	return svc, meta, storage
}

func TestCreateSession_Success(t *testing.T) {
	svc, meta, _ := newTestUploadService(t)
	ctx := context.Background()

	session, err := svc.CreateSession(ctx, "sha256-abc", 1000, CompZstd, 256, "demo", "test.bin")
	if err != nil {
		t.Fatalf("CreateSession error = %v", err)
	}

	if session.SessionID == "" {
		t.Error("SessionID 为空")
	}
	if session.UploadLength != 1000 {
		t.Errorf("UploadLength = %d, want 1000", session.UploadLength)
	}
	if session.Compression != CompZstd {
		t.Errorf("Compression = %s, want zstd", session.Compression)
	}
	if session.Namespace != "demo" {
		t.Errorf("Namespace = %s", session.Namespace)
	}
	if session.Status != SessionActive {
		t.Errorf("Status = %s, want active", session.Status)
	}
	if session.ChunkSize != 256 {
		t.Errorf("ChunkSize = %d, want 256", session.ChunkSize)
	}

	// 验证已存到 meta
	got, _ := meta.GetSession(ctx, session.SessionID)
	if got == nil {
		t.Error("会话未保存到 meta")
	}
}

func TestCreateSession_ZeroLength(t *testing.T) {
	svc, _, _ := newTestUploadService(t)
	ctx := context.Background()

	session, err := svc.CreateSession(ctx, "sha", 0, CompNone, 0, "ns", "")
	if err != nil {
		t.Fatalf("CreateSession(0 length) err = %v, want nil", err)
	}
	if session.UploadLength != 0 {
		t.Errorf("UploadLength = %d, want 0", session.UploadLength)
	}
}

func TestCreateSession_DefaultChunkSize(t *testing.T) {
	svc, _, _ := newTestUploadService(t)
	ctx := context.Background()

	session, err := svc.CreateSession(ctx, "sha", 100, CompNone, 0, "ns", "")
	if err != nil {
		t.Fatalf("CreateSession error = %v", err)
	}
	if session.ChunkSize != 1024*1024 {
		t.Errorf("DefaultChunkSize = %d, want %d", session.ChunkSize, 1024*1024)
	}
}

func TestCheckExists_Miss(t *testing.T) {
	svc, _, _ := newTestUploadService(t)
	ctx := context.Background()

	result, err := svc.CheckExists(ctx, "nonexistent-sha", "demo", "f.txt")
	if err != nil {
		t.Fatalf("CheckExists error = %v", err)
	}
	if result != nil {
		t.Error("不存在的 sha256 应返回 nil")
	}
}

func TestCheckExists_Hit(t *testing.T) {
	svc, meta, _ := newTestUploadService(t)
	ctx := context.Background()

	// 先创建 blob
	blob := &ContentBlob{
		SHA256: "existing-sha", StoragePath: "ns/f1",
		Size: 500, RefCount: 1, CreatedAt: time.Now(),
	}
	meta.PutBlob(ctx, blob)

	result, err := svc.CheckExists(ctx, "existing-sha", "demo", "dup.txt")
	if err != nil {
		t.Fatalf("CheckExists error = %v", err)
	}
	if result == nil {
		t.Fatal("命中秒传应返回 file")
	}
	if result.SHA256 != "existing-sha" {
		t.Errorf("SHA256 = %s", result.SHA256)
	}
	if result.Size != 500 {
		t.Errorf("Size = %d", result.Size)
	}

	// 验证引用计数增加
	updated, _ := meta.GetBlobBySha(ctx, "existing-sha")
	if updated.RefCount != 2 {
		t.Errorf("RefCount = %d, want 2", updated.RefCount)
	}
}

func TestCheckExists_EmptySHA(t *testing.T) {
	svc, _, _ := newTestUploadService(t)
	ctx := context.Background()

	_, err := svc.CheckExists(ctx, "", "ns", "")
	if err != ErrInvalidArgument {
		t.Errorf("空 SHA 应返回 ErrInvalidArgument, got %v", err)
	}
}

func TestAppendChunk_Success(t *testing.T) {
	svc, _, _ := newTestUploadService(t)
	ctx := context.Background()

	session, _ := svc.CreateSession(ctx, "sha", 1000, CompNone, 256, "demo", "f.bin")

	err := svc.AppendChunk(ctx, session.SessionID, 0, strings.NewReader("hello chunk"), "")
	if err != nil {
		t.Fatalf("AppendChunk error = %v", err)
	}
}

func TestAppendChunk_SessionNotFound(t *testing.T) {
	svc, _, _ := newTestUploadService(t)
	ctx := context.Background()

	err := svc.AppendChunk(ctx, "no-such-session", 0, strings.NewReader("data"), "")
	if err != ErrSessionNotFound {
		t.Errorf("AppendChunk(no session) err = %v, want ErrSessionNotFound", err)
	}
}

func TestGetOffset(t *testing.T) {
	svc, _, _ := newTestUploadService(t)
	ctx := context.Background()

	session, _ := svc.CreateSession(ctx, "sha", 100, CompNone, 10, "ns", "")
	// 不传 declaredSha256，跳过校验
	svc.AppendChunk(ctx, session.SessionID, 0, strings.NewReader("0123456789"), "")
	svc.AppendChunk(ctx, session.SessionID, 1, strings.NewReader("abcdefghij"), "")

	offset, err := svc.GetOffset(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("GetOffset error = %v", err)
	}
	if offset != 20 {
		t.Errorf("Offset = %d, want 20", offset)
	}
}

func TestGetOffset_SessionNotFound(t *testing.T) {
	svc, _, _ := newTestUploadService(t)
	ctx := context.Background()

	_, err := svc.GetOffset(ctx, "no-such")
	if err != ErrSessionNotFound {
		t.Errorf("GetOffset(no session) err = %v, want ErrSessionNotFound", err)
	}
}

func TestGetStatus(t *testing.T) {
	svc, _, _ := newTestUploadService(t)
	ctx := context.Background()

	session, _ := svc.CreateSession(ctx, "sha", 100, CompNone, 10, "ns", "")
	svc.AppendChunk(ctx, session.SessionID, 0, strings.NewReader("chunk0data"), "")

	chunks, total, err := svc.GetStatus(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("GetStatus error = %v", err)
	}
	if len(chunks) != 1 {
		t.Errorf("chunks count = %d, want 1", len(chunks))
	}
	if total <= 0 {
		t.Errorf("total = %d, want > 0", total)
	}
}

func TestFinalize_Success(t *testing.T) {
	svc, meta, storage := newTestUploadService(t)
	ctx := context.Background()

	// 数据
	content := []byte("Hello, 世界! This is the final content for checksum.")
	sha := sha256Hex(content)

	session, _ := svc.CreateSession(ctx, sha, int64(len(content)), CompNone, 100, "demo", "finalize.txt")

	// 分片上传
	svc.AppendChunk(ctx, session.SessionID, 0, bytes.NewReader(content), "")

	// Finalize
	result, err := svc.Finalize(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("Finalize error = %v", err)
	}

	if result == nil {
		t.Fatal("Finalize result is nil")
	}
	if result.SHA256 != sha {
		t.Errorf("SHA256 = %s, want %s", result.SHA256, sha)
	}
	if result.Size != int64(len(content)) {
		t.Errorf("Size = %d, want %d", result.Size, len(content))
	}
	if result.Name != "finalize.txt" {
		t.Errorf("Name = %s", result.Name)
	}
	if result.Namespace != "demo" {
		t.Errorf("Namespace = %s", result.Namespace)
	}

	// 验证 blob 被创建
	if !meta.hasBlob(sha) {
		t.Error("Finalize 后 blob 未创建")
	}

	// 验证 file 被创建
	if !meta.hasFile(result.FileID) {
		t.Error("Finalize 后 file 未创建")
	}

	// 验证存储有数据（使用 mock 的 Stat 方法，线程安全）
	_, exists, err := storage.Stat(ctx, "demo/finalize.txt")
	if err != nil {
		t.Fatalf("Stat error: %v", err)
	}
	if !exists {
		t.Error("Finalize 后存储中没有文件")
	}
}

func TestFinalize_ChecksumMismatch(t *testing.T) {
	svc, meta, storage := newTestUploadService(t)
	ctx := context.Background()

	content := []byte("content to upload")
	wrongSha := "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"

	session, _ := svc.CreateSession(ctx, wrongSha, int64(len(content)), CompNone, 100, "demo", "bad.txt")
	svc.AppendChunk(ctx, session.SessionID, 0, bytes.NewReader(content), "")

	_, err := svc.Finalize(ctx, session.SessionID)
	if err != ErrContentChecksum {
		t.Errorf("Finalize(checksum mismatch) err = %v, want ErrContentChecksum", err)
	}

	// 验证没有创建 blob
	allBlobs, _ := meta.ListAllBlobs(ctx)
	if len(allBlobs) > 0 {
		t.Errorf("校验失败后仍有 blob 被创建: %d", len(allBlobs))
	}

	_ = storage
}

func TestFinalize_NoChunksZeroByte(t *testing.T) {
	svc, meta, _ := newTestUploadService(t)
	ctx := context.Background()

	// 创建 0 字节上传会话
	session, _ := svc.CreateSession(ctx, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", 0, CompNone, 10, "ns", "empty.txt")

	// 不传任何分片，直接 Finalize → 应成功创建空文件
	result, err := svc.Finalize(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("Finalize(0 bytes) err = %v, want nil", err)
	}
	if result.Size != 0 {
		t.Errorf("Size = %d, want 0", result.Size)
	}
	if result.Name != "empty.txt" {
		t.Errorf("Name = %s, want empty.txt", result.Name)
	}

	// 验证 blob 被创建
	blob, _ := meta.GetBlobBySha(ctx, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
	if blob == nil {
		t.Error("0 字节文件 Finalize 后 blob 未创建")
	}
	if blob.Size != 0 {
		t.Errorf("blob Size = %d, want 0", blob.Size)
	}
}

func TestFinalize_SessionNotFound(t *testing.T) {
	svc, _, _ := newTestUploadService(t)
	ctx := context.Background()

	_, err := svc.Finalize(ctx, "no-such")
	if err != ErrSessionNotFound {
		t.Errorf("Finalize(no session) err = %v, want ErrSessionNotFound", err)
	}
}

func TestFinalize_MultipleChunks(t *testing.T) {
	svc, meta, storage := newTestUploadService(t)
	ctx := context.Background()

	// 准备多分片数据
	chunk0 := []byte("0123456789")
	chunk1 := []byte("abcdefghij")
	fullContent := append(chunk0, chunk1...)
	sha := sha256Hex(fullContent)

	session, _ := svc.CreateSession(ctx, sha, int64(len(fullContent)), CompNone, 10, "demo", "multi.bin")

	svc.AppendChunk(ctx, session.SessionID, 0, bytes.NewReader(chunk0), "")
	svc.AppendChunk(ctx, session.SessionID, 1, bytes.NewReader(chunk1), "")

	result, err := svc.Finalize(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("Finalize(multi) error = %v", err)
	}

	if result.SHA256 != sha {
		t.Errorf("SHA256 mismatch: got %s, want %s", result.SHA256, sha)
	}
	if result.Size != int64(len(fullContent)) {
		t.Errorf("Size = %d, want %d", result.Size, len(fullContent))
	}

	_ = meta
	_ = storage
}

func TestSubmitDir(t *testing.T) {
	svc, meta, _ := newTestUploadService(t)
	ctx := context.Background()

	// 先创建一个文件
	file := &FileMetadata{
		FileID: "child-file", SHA256: "child-sha", Name: "a.txt",
		Path: "a.txt", Size: 100, Namespace: "demo", CreatedAt: time.Now(),
	}
	meta.PutFile(ctx, file)

	manifest := DirManifest{
		Entries: []DirEntry{
			{Path: "docs/a.txt", FileID: "child-file"},
		},
	}

	dir, err := svc.SubmitDir(ctx, manifest, "demo")
	if err != nil {
		t.Fatalf("SubmitDir error = %v", err)
	}
	if dir == nil {
		t.Fatal("SubmitDir result is nil")
	}
	if !dir.IsDir {
		t.Error("SubmitDir result should be dir")
	}

	// 验证子节点
	children, _ := meta.ListChildren(ctx, dir.FileID)
	if len(children) != 1 {
		t.Errorf("children count = %d, want 1", len(children))
	}
}

func TestDelete_SingleFile(t *testing.T) {
	svc, meta, storage := newTestUploadService(t)
	ctx := context.Background()

	// 准备 blob
	blob := &ContentBlob{
		SHA256: "del-sha", StoragePath: "ns/del-file",
		Size: 100, RefCount: 1, CreatedAt: time.Now(),
	}
	meta.PutBlob(ctx, blob)
	storage.Write(ctx, "ns/del-file", bytes.NewReader([]byte("data")))

	file := &FileMetadata{
		FileID: "del-file", SHA256: "del-sha", Name: "del.txt",
		Path: "del.txt", Size: 100, Namespace: "demo", CreatedAt: time.Now(),
	}
	meta.PutFile(ctx, file)

	err := svc.Delete(ctx, "del-file", "demo")
	if err != nil {
		t.Fatalf("Delete error = %v", err)
	}

	// 验证 file 被删除
	if meta.hasFile("del-file") {
		t.Error("Delete 后 file 仍存在")
	}

	// 验证引用计数归零、物理文件被删
	updated, _ := meta.GetBlobBySha(ctx, "del-sha")
	if updated == nil || updated.RefCount != 0 {
		t.Errorf("RefCount = %d, want 0", updated.RefCount)
	}
	if storage.has("ns/del-file") {
		t.Error("引用归零后物理文件未被删除")
	}
}

func TestDelete_SharedBlob(t *testing.T) {
	svc, meta, storage := newTestUploadService(t)
	ctx := context.Background()

	blob := &ContentBlob{
		SHA256: "shared", StoragePath: "ns/shared",
		Size: 50, RefCount: 2, CreatedAt: time.Now(),
	}
	meta.PutBlob(ctx, blob)
	storage.Write(ctx, "ns/shared", bytes.NewReader([]byte("data")))

	f1 := &FileMetadata{FileID: "f1", SHA256: "shared", Name: "f1.txt", Path: "f1", Size: 50, Namespace: "demo", CreatedAt: time.Now()}
	f2 := &FileMetadata{FileID: "f2", SHA256: "shared", Name: "f2.txt", Path: "f2", Size: 50, Namespace: "demo", CreatedAt: time.Now()}
	meta.PutFile(ctx, f1)
	meta.PutFile(ctx, f2)

	// 删 f1，物理文件应保留（ref_count 1→2→1）
	err := svc.Delete(ctx, "f1", "demo")
	if err != nil {
		t.Fatalf("Delete f1 error = %v", err)
	}

	// blob 应仍在，ref_count=1
	updated, _ := meta.GetBlobBySha(ctx, "shared")
	if updated == nil || updated.RefCount != 1 {
		t.Errorf("After delete RefCount = %d, want 1", updated.RefCount)
	}
	if !storage.has("ns/shared") {
		t.Error("共享 blob 被误删")
	}
}

func TestDelete_DirRecursive(t *testing.T) {
	svc, meta, _ := newTestUploadService(t)
	ctx := context.Background()

	now := time.Now()
	dir := &FileMetadata{FileID: "parent-dir", Name: "root", Path: "/", Namespace: "demo", IsDir: true, CreatedAt: now}
	meta.PutFile(ctx, dir)

	child := &FileMetadata{FileID: "child-file", Name: "c.txt", Path: "c.txt", Size: 10, Namespace: "demo", ParentID: "parent-dir", CreatedAt: now}
	meta.PutFile(ctx, child)

	err := svc.Delete(ctx, "parent-dir", "demo")
	if err != nil {
		t.Fatalf("Delete dir error = %v", err)
	}

	if meta.hasFile("parent-dir") {
		t.Error("目录删除后 dir 仍存在")
	}
	if meta.hasFile("child-file") {
		t.Error("目录递归删除后子文件仍存在")
	}
}

func TestDelete_Forbidden(t *testing.T) {
	svc, meta, _ := newTestUploadService(t)
	ctx := context.Background()

	file := &FileMetadata{
		FileID: "f1", Name: "test.txt", Path: "test",
		Size: 10, Namespace: "other-ns", CreatedAt: time.Now(),
	}
	meta.PutFile(ctx, file)

	err := svc.Delete(ctx, "f1", "wrong-ns")
	if err != ErrForbidden {
		t.Errorf("Delete wrong ns err = %v, want ErrForbidden", err)
	}
}

func TestDelete_NotFound(t *testing.T) {
	svc, _, _ := newTestUploadService(t)
	ctx := context.Background()

	err := svc.Delete(ctx, "no-such", "ns")
	if err != ErrNotFound {
		t.Errorf("Delete not found err = %v, want ErrNotFound", err)
	}
}

func TestAbort(t *testing.T) {
	svc, meta, _ := newTestUploadService(t)
	ctx := context.Background()

	session, _ := svc.CreateSession(ctx, "sha", 100, CompNone, 10, "ns", "f.txt")
	svc.AppendChunk(ctx, session.SessionID, 0, strings.NewReader("data"), "")

	err := svc.Abort(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("Abort error = %v", err)
	}

	// 验证会话被删除
	got, _ := meta.GetSession(ctx, session.SessionID)
	if got != nil {
		t.Error("Abort 后会话仍存在")
	}
}

func TestAppendChunk_WrongState(t *testing.T) {
	svc, meta, _ := newTestUploadService(t)
	ctx := context.Background()

	session, _ := svc.CreateSession(ctx, "sha", 100, CompNone, 10, "ns", "")
	// 手动标记为 completed
	session.Status = SessionCompleted
	meta.CreateSession(ctx, session)

	err := svc.AppendChunk(ctx, session.SessionID, 0, strings.NewReader("data"), "")
	if err != ErrSessionState {
		t.Errorf("completed session AppendChunk err = %v, want ErrSessionState", err)
	}
}

// 辅助
func sha256Hex(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// 覆盖 TestNewHashAccumulator
func TestMockHashAccumulator(t *testing.T) {
	acc := NewMockHashAccumulator()
	data := []byte("accumulator test")
	acc.Write(data)

	if acc.N() != int64(len(data)) {
		t.Errorf("N() = %d, want %d", acc.N(), len(data))
	}
	if acc.SumHex() == "" {
		t.Error("SumHex() is empty")
	}
}
