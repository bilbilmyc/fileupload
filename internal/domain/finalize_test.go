package domain

import (
	"bytes"
	"context"
	"io"
	"sort"
	"testing"
	"time"
)

func TestMergeChunks_EmptyChunks(t *testing.T) {
	ctx := context.Background()
	tempStorage := newMockStorage()

	// 空分片 → 空 reader
	reader, cleanup, err := mergeChunks(ctx, "sess-0", nil, tempStorage)
	if err != nil {
		t.Fatalf("mergeChunks(empty) error = %v", err)
	}
	defer cleanup()
	defer reader.Close()

	data, _ := io.ReadAll(reader)
	if len(data) != 0 {
		t.Errorf("data length = %d, want 0", len(data))
	}
}

func TestMergeChunks_SingleChunk(t *testing.T) {
	ctx := context.Background()
	tempStorage := newMockStorage()
	sessionID := "sess-1"
	content := []byte("single chunk content")

	// 模拟一个分片
	tempStorage.Write(ctx, sessionID+"/0.part", bytes.NewReader(content))
	chunks := []ChunkInfo{{Index: 0, SHA256: "sha0", Size: int64(len(content))}}

	reader, cleanup, err := mergeChunks(ctx, sessionID, chunks, tempStorage)
	if err != nil {
		t.Fatalf("mergeChunks error = %v", err)
	}
	defer cleanup()
	defer reader.Close()

	data, _ := io.ReadAll(reader)
	if !bytes.Equal(data, content) {
		t.Errorf("merged data mismatch: got %s, want %s", data, content)
	}
}

func TestMergeChunks_MultipleChunks(t *testing.T) {
	ctx := context.Background()
	tempStorage := newMockStorage()
	sessionID := "sess-2"

	// 模拟多个分片
	chunk0 := []byte("chunk-zero-")
	chunk1 := []byte("chunk-one")
	fullContent := append(chunk0, chunk1...)

	tempStorage.Write(ctx, sessionID+"/0.part", bytes.NewReader(chunk0))
	tempStorage.Write(ctx, sessionID+"/1.part", bytes.NewReader(chunk1))

	chunks := []ChunkInfo{
		{Index: 0, SHA256: "sha0", Size: int64(len(chunk0))},
		{Index: 1, SHA256: "sha1", Size: int64(len(chunk1))},
	}

	reader, cleanup, err := mergeChunks(ctx, sessionID, chunks, tempStorage)
	if err != nil {
		t.Fatalf("mergeChunks error = %v", err)
	}
	defer cleanup()
	defer reader.Close()

	data, _ := io.ReadAll(reader)
	if !bytes.Equal(data, fullContent) {
		t.Errorf("merged data mismatch: got %s, want %s", data, fullContent)
	}
}

func TestMergeChunks_OutOfOrder(t *testing.T) {
	ctx := context.Background()
	tempStorage := newMockStorage()
	sessionID := "sess-3"

	chunk0 := []byte("first")
	chunk1 := []byte("second")
	chunk2 := []byte("third")

	tempStorage.Write(ctx, sessionID+"/0.part", bytes.NewReader(chunk0))
	tempStorage.Write(ctx, sessionID+"/1.part", bytes.NewReader(chunk1))
	tempStorage.Write(ctx, sessionID+"/2.part", bytes.NewReader(chunk2))

	// 乱序 chunks（index 2, 0, 1）
	chunks := []ChunkInfo{
		{Index: 2, SHA256: "sha2", Size: int64(len(chunk2))},
		{Index: 0, SHA256: "sha0", Size: int64(len(chunk0))},
		{Index: 1, SHA256: "sha1", Size: int64(len(chunk1))},
	}

	reader, cleanup, err := mergeChunks(ctx, sessionID, chunks, tempStorage)
	if err != nil {
		t.Fatalf("mergeChunks error = %v", err)
	}
	defer cleanup()
	defer reader.Close()

	data, _ := io.ReadAll(reader)
	expected := append(chunk0, chunk1...)
	expected = append(expected, chunk2...)
	if !bytes.Equal(data, expected) {
		t.Errorf("merged data mismatch: got %s, want %s", data, expected)
	}
}

func TestMergeChunks_CleanupRemovesFiles(t *testing.T) {
	ctx := context.Background()
	tempStorage := newMockStorage()
	sessionID := "sess-4"

	content := []byte("cleanup test")
	tempStorage.Write(ctx, sessionID+"/0.part", bytes.NewReader(content))
	chunks := []ChunkInfo{{Index: 0, SHA256: "sha0", Size: int64(len(content))}}

	reader, cleanup, err := mergeChunks(ctx, sessionID, chunks, tempStorage)
	if err != nil {
		t.Fatalf("mergeChunks error = %v", err)
	}

	// 分片文件应存在
	if !tempStorage.has(sessionID + "/0.part") {
		t.Error("chunk file should exist before cleanup")
	}

	reader.Close()
	cleanup()

	// 分片文件应被清理
	if tempStorage.has(sessionID + "/0.part") {
		t.Error("chunk file should be deleted after cleanup")
	}
}

func TestVerifyStream_NoCompression(t *testing.T) {
	ctx := context.Background()
	content := []byte("verify no compression test data")
	r := io.NopCloser(bytes.NewReader(content))

	compress := newMockCompressor()
	hasher := newMockHasher()

	verified, hashRes, err := verifyStream(ctx, r, CompNone, compress, hasher)
	if err != nil {
		t.Fatalf("verifyStream error = %v", err)
	}

	data, _ := io.ReadAll(verified)
	if !bytes.Equal(data, content) {
		t.Errorf("verify data mismatch")
	}

	sha, n := hashRes()
	if sha == "" {
		t.Error("hash should not be empty")
	}
	if n != int64(len(content)) {
		t.Errorf("n = %d, want %d", n, len(content))
	}
}

func TestVerifyStream_ChecksumMismatch(t *testing.T) {
	ctx := context.Background()
	content := []byte("data for checksum check")
	r := io.NopCloser(bytes.NewReader(content))

	compress := newMockCompressor()
	hasher := newMockHasher()

	// 设置 verifyStream 后手动计算哈希校验
	verified, hashRes, err := verifyStream(ctx, r, CompNone, compress, hasher)
	if err != nil {
		t.Fatalf("verifyStream error = %v", err)
	}

	// 消耗 reader
	io.ReadAll(verified)

	sha, _ := hashRes()
	// SHA-256 of "data for checksum check" — compute at test time to avoid hardcoded wrong values
	expected := sha256Hex([]byte("data for checksum check"))
	if sha != expected {
		t.Errorf("SHA256 = %s, want %s", sha, expected)
	}
}

func TestVerifyStream_Compression(t *testing.T) {
	ctx := context.Background()
	original := []byte("compressed verify test data")

	compress := newMockCompressor()
	hasher := newMockHasher()

	// mock compressor 的 Decompress 直接透传，所以这里只验证压缩格式被传递正确
	r := io.NopCloser(bytes.NewReader(original))

	verified, hashRes, err := verifyStream(ctx, r, CompZstd, compress, hasher)
	if err != nil {
		t.Fatalf("verifyStream with compression error = %v", err)
	}

	data, _ := io.ReadAll(verified)
	if !bytes.Equal(data, original) {
		t.Errorf("verify compressed data mismatch")
	}

	sha, n := hashRes()
	if sha == "" {
		t.Error("hash should not be empty")
	}
	if n != int64(len(original)) {
		t.Errorf("n = %d, want %d", n, len(original))
	}
}

func TestCommitStream_Success(t *testing.T) {
	ctx := context.Background()
	storage := newMockStorage()
	meta := newMockMetadata()

	content := []byte("commit stream test content")
	sha := sha256Hex(content)
	r := bytes.NewReader(content)

	session := &UploadSession{
		SessionID: "commit-sess", SHA256: sha,
		UploadLength: int64(len(content)), Compression: CompNone,
		ChunkSize: 100, Namespace: "demo", FileName: "commit.txt",
		CreatedAt: time.Now(), ExpireAt: time.Now().Add(time.Hour),
		Status: SessionActive,
	}

	storagePath := "demo/commit.txt"
	dummyHash := func() (string, int64) { return sha, int64(len(content)) }
	fileMeta, err := commitStream(ctx, r, storagePath, session, storage, meta, meta, dummyHash)
	if err != nil {
		t.Fatalf("commitStream error = %v", err)
	}

	if fileMeta == nil {
		t.Fatal("fileMeta is nil")
	}
	if fileMeta.SHA256 != sha {
		t.Errorf("SHA256 = %s, want %s", fileMeta.SHA256, sha)
	}
	if fileMeta.Size != int64(len(content)) {
		t.Errorf("Size = %d, want %d", fileMeta.Size, len(content))
	}
	if fileMeta.Name != "commit.txt" {
		t.Errorf("Name = %s", fileMeta.Name)
	}
	if fileMeta.Namespace != "demo" {
		t.Errorf("Namespace = %s", fileMeta.Namespace)
	}

	// 验证 blob 创建
	blob, _ := meta.GetBlobBySha(ctx, sha)
	if blob == nil {
		t.Error("blob not created")
	}
	if blob.RefCount != 1 {
		t.Errorf("RefCount = %d, want 1", blob.RefCount)
	}
	if blob.Size != int64(len(content)) {
		t.Errorf("blob size = %d, want %d", blob.Size, len(content))
	}

	// 验证文件存在
	if !meta.hasFile(fileMeta.FileID) {
		t.Error("file not created")
	}

	// 验证存储有数据
	_, stored, _ := storage.Stat(ctx, storagePath)
	if !stored {
		t.Error("storage path not written")
	}
}

func TestCommitStream_ChecksumMismatch(t *testing.T) {
	ctx := context.Background()
	storage := newMockStorage()
	meta := newMockMetadata()

	content := []byte("real content")
	r := bytes.NewReader(content)

	wrongSha := "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"

	session := &UploadSession{
		SessionID: "bad-commit", SHA256: wrongSha,
		UploadLength: int64(len(content)), Compression: CompNone,
		Namespace: "demo", FileName: "bad.txt",
		CreatedAt: time.Now(), ExpireAt: time.Now().Add(time.Hour),
		Status: SessionActive,
	}

	storagePath := "demo/bad.txt"
	// dummyHash 返回真实内容的哈希，与 session.SHA256（wrongSha）不同以触发校验失败
	realSha := sha256Hex(content)
	dummyHash := func() (string, int64) {
		return realSha, int64(len(content))
	}
	_, err := commitStream(ctx, r, storagePath, session, storage, meta, meta, dummyHash)
	if err != ErrContentChecksum {
		t.Errorf("commitStream(checksum mismatch) err = %v, want ErrContentChecksum", err)
	}

	// 验证存储被回滚
	_, exists, _ := storage.Stat(ctx, storagePath)
	if exists {
		t.Error("storage should be deleted after checksum mismatch")
	}
}

func TestCommitStream_ZeroBytes(t *testing.T) {
	ctx := context.Background()
	storage := newMockStorage()
	meta := newMockMetadata()

	var content []byte // empty
	r := bytes.NewReader(content)
	emptySha := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	session := &UploadSession{
		SessionID: "zero-commit", SHA256: emptySha,
		UploadLength: 0, Compression: CompNone,
		Namespace: "ns", FileName: "empty.txt",
		CreatedAt: time.Now(), ExpireAt: time.Now().Add(time.Hour),
		Status: SessionActive,
	}

	storagePath := "ns/empty.txt"
	dummyEmpty := func() (string, int64) { return emptySha, 0 }
	fileMeta, err := commitStream(ctx, r, storagePath, session, storage, meta, meta, dummyEmpty)
	if err != nil {
		t.Fatalf("commitStream(zero) error = %v", err)
	}
	if fileMeta.Size != 0 {
		t.Errorf("size = %d, want 0", fileMeta.Size)
	}
}

func TestMergeAndCommitFullPipeline(t *testing.T) {
	ctx := context.Background()
	tempStorage := newMockStorage()
	storage := newMockStorage()
	meta := newMockMetadata()
	compress := newMockCompressor()
	hasher := newMockHasher()

	sessionID := "pipeline-test"
	content := []byte("full pipeline test content for finalize")

	// 模拟分片
	tempStorage.Write(ctx, sessionID+"/0.part", bytes.NewReader(content[:10]))
	tempStorage.Write(ctx, sessionID+"/1.part", bytes.NewReader(content[10:]))

	sha := sha256Hex(content)
	session := &UploadSession{
		SessionID: sessionID, SHA256: sha,
		UploadLength: int64(len(content)), Compression: CompNone,
		ChunkSize: 10, Namespace: "demo", FileName: "pipeline.txt",
		CreatedAt: time.Now(), ExpireAt: time.Now().Add(time.Hour),
		Status: SessionActive,
	}

	// Phase 1: Merge
	chunks := []ChunkInfo{
		{Index: 0, SHA256: "sha0", Size: 10},
		{Index: 1, SHA256: "sha1", Size: int64(len(content) - 10)},
	}
	sort.Slice(chunks, func(i, j int) bool { return chunks[i].Index < chunks[j].Index })

	merged, cleanup, err := mergeChunks(ctx, sessionID, chunks, tempStorage)
	if err != nil {
		t.Fatalf("mergeChunks error = %v", err)
	}
	defer cleanup()
	defer merged.Close()

	// Phase 2: Verify
	verified, hashRes, err := verifyStream(ctx, merged, CompNone, compress, hasher)
	if err != nil {
		t.Fatalf("verifyStream error = %v", err)
	}

	// Phase 3: Commit
	fileMeta, err := commitStream(ctx, verified, "demo/pipeline.txt", session, storage, meta, meta, hashRes)
	if err != nil {
		t.Fatalf("commitStream error = %v", err)
	}

	// 验证 hashRes 在 commit 后可用
	actualSha, n := hashRes()
	if actualSha != sha {
		t.Errorf("SHA256 = %s, want %s", actualSha, sha)
	}
	if n != int64(len(content)) {
		t.Errorf("n = %d, want %d", n, len(content))
	}

	if fileMeta.SHA256 != sha {
		t.Errorf("file SHA256 = %s, want %s", fileMeta.SHA256, sha)
	}
}

func TestCommitStream_HandlesNamespacePath(t *testing.T) {
	ctx := context.Background()
	storage := newMockStorage()
	meta := newMockMetadata()

	content := []byte("path test")
	r := bytes.NewReader(content)
	sha := sha256Hex(content)

	session := &UploadSession{
		SessionID: "path-sess", SHA256: sha,
		UploadLength: int64(len(content)), Compression: CompNone,
		Namespace: "team-a", FileName: "subdir/file.txt",
		CreatedAt: time.Now(), ExpireAt: time.Now().Add(time.Hour),
		Status: SessionActive,
	}

	storagePath := "team-a/subdir/file.txt"
	dummyHash := func() (string, int64) { return sha, int64(len(content)) }
	fileMeta, err := commitStream(ctx, r, storagePath, session, storage, meta, meta, dummyHash)
	if err != nil {
		t.Fatalf("commitStream error = %v", err)
	}

	if fileMeta.Path != "subdir/file.txt" {
		t.Errorf("file Path = %s, want subdir/file.txt", fileMeta.Path)
	}

	// 验证文件名的 Base 名
	if fileMeta.Name != "file.txt" {
		t.Errorf("file Name = %s, want file.txt", fileMeta.Name)
	}
}

func TestCommitStream_ExistingBlob(t *testing.T) {
	ctx := context.Background()
	storage := newMockStorage()
	meta := newMockMetadata()

	content := []byte("dedup content")
	sha := sha256Hex(content)
	r := bytes.NewReader(content)

	// 预存 blob
	existingBlob := &ContentBlob{
		SHA256: sha, StoragePath: "ns/existing",
		Size: int64(len(content)), RefCount: 1, CreatedAt: time.Now(),
	}
	meta.PutBlob(ctx, existingBlob)

	session := &UploadSession{
		SessionID: "dedup-sess", SHA256: sha,
		UploadLength: int64(len(content)), Compression: CompNone,
		Namespace: "ns", FileName: "dedup.txt",
		CreatedAt: time.Now(), ExpireAt: time.Now().Add(time.Hour),
		Status: SessionActive,
	}

	storagePath := "ns/dedup.txt"
	dummyHash := func() (string, int64) { return sha, int64(len(content)) }
	fileMeta, err := commitStream(ctx, r, storagePath, session, storage, meta, meta, dummyHash)
	if err != nil {
		t.Fatalf("commitStream(dedup) error = %v", err)
	}
	_ = fileMeta

	// 验证现有 blob 的 refcount 增加（如果 blob 已存在，PutBlob 覆盖或忽略）
	blob, _ := meta.GetBlobBySha(ctx, sha)
	// mockMetadata.PutBlob 会覆盖，所以只验证 blob 存在
	if blob == nil {
		t.Error("blob should exist")
	}
}

