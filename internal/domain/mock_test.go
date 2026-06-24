package domain

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"sync"
	"time"
)

// ===== Mock Storage (thread-safe) =====

type mockStorage struct {
	mu    sync.Mutex
	files map[string][]byte
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		files: make(map[string][]byte),
	}
}

func (m *mockStorage) Write(_ context.Context, path string, r io.Reader) (int64, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return 0, err
	}
	m.mu.Lock()
	m.files[path] = data
	m.mu.Unlock()
	return int64(len(data)), nil
}

func (m *mockStorage) Open(_ context.Context, path string, offset, length int64) (io.ReadCloser, error) {
	m.mu.Lock()
	data, ok := m.files[path]
	m.mu.Unlock()
	if !ok {
		return nil, ErrNotFound
	}
	start := int(offset)
	end := len(data)
	if length > 0 && start+int(length) < end {
		end = start + int(length)
	}
	if start >= len(data) {
		return nil, ErrInvalidArgument
	}
	return io.NopCloser(bytes.NewReader(data[start:end])), nil
}

func (m *mockStorage) Delete(_ context.Context, path string) error {
	m.mu.Lock()
	delete(m.files, path)
	m.mu.Unlock()
	return nil
}

func (m *mockStorage) Stat(_ context.Context, path string) (int64, bool, error) {
	m.mu.Lock()
	data, ok := m.files[path]
	m.mu.Unlock()
	if !ok {
		return 0, false, nil
	}
	return int64(len(data)), true, nil
}

func (m *mockStorage) has(path string) bool {
	m.mu.Lock()
	_, ok := m.files[path]
	m.mu.Unlock()
	return ok
}

func (m *mockStorage) Walk(_ context.Context, fn func(path string, info fs.FileInfo) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for path, data := range m.files {
		if err := fn(path, mockFileInfo{name: path, size: int64(len(data))}); err != nil {
			return err
		}
	}
	return nil
}

type mockFileInfo struct{ name string; size int64 }

func (m mockFileInfo) Name() string       { return m.name }
func (m mockFileInfo) Size() int64        { return m.size }
func (m mockFileInfo) Mode() fs.FileMode  { return 0 }
func (m mockFileInfo) ModTime() time.Time { return time.Time{} }
func (m mockFileInfo) IsDir() bool        { return false }
func (m mockFileInfo) Sys() any           { return nil }

// ===== Mock Metadata (thread-safe) =====

type mockMetadata struct {
	mu         sync.Mutex
	sessions   map[string]*UploadSession
	sessionTTL map[string]time.Duration
	chunks     map[string][]ChunkInfo
	blobs      map[string]*ContentBlob
	files      map[string]*FileMetadata

	// 失败注入：deleteFileCount > failAfterDeleteFile 时 DeleteFile 返回错误（0 = 不注入）
	deleteFileCount     int
	failAfterDeleteFile int
}

func newMockMetadata() *mockMetadata {
	return &mockMetadata{
		sessions:   make(map[string]*UploadSession),
		sessionTTL: make(map[string]time.Duration),
		chunks:     make(map[string][]ChunkInfo),
		blobs:      make(map[string]*ContentBlob),
		files:      make(map[string]*FileMetadata),
	}
}

func (m *mockMetadata) CreateSession(_ context.Context, s *UploadSession) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[s.SessionID] = s
	return nil
}

func (m *mockMetadata) GetSession(_ context.Context, id string) (*UploadSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, nil
	}
	if s.ExpireAt.Before(time.Now()) && s.Status != SessionCompleted {
		return nil, nil
	}
	return s, nil
}

func (m *mockMetadata) UpdateOffset(_ context.Context, id string, sliceIndex int, sliceSha string, addBytes int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.chunks[id] = append(m.chunks[id], ChunkInfo{
		Index:  sliceIndex,
		SHA256: sliceSha,
		Size:   addBytes,
	})
	return nil
}

func (m *mockMetadata) ListChunks(_ context.Context, id string) ([]ChunkInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]ChunkInfo{}, m.chunks[id]...), nil
}

func (m *mockMetadata) DeleteSession(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
	delete(m.chunks, id)
	delete(m.sessionTTL, id)
	return nil
}

func (m *mockMetadata) TouchSession(_ context.Context, id string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionTTL[id] = ttl
	if s, ok := m.sessions[id]; ok {
		s.ExpireAt = time.Now().Add(ttl)
	}
	return nil
}

func (m *mockMetadata) ListExpiredSessions(_ context.Context) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var expired []string
	for id, s := range m.sessions {
		if s.ExpireAt.Before(time.Now()) && s.Status != SessionFinalizing {
			expired = append(expired, id)
		}
	}
	return expired, nil
}

func (m *mockMetadata) GetBlobBySha(_ context.Context, sha256 string) (*ContentBlob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.blobs[sha256]
	if !ok {
		return nil, nil
	}
	return b, nil
}

func (m *mockMetadata) PutBlob(_ context.Context, b *ContentBlob) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blobs[b.SHA256] = b
	return nil
}

func (m *mockMetadata) UpdateBlobStorage(_ context.Context, sha256 string, storagePath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if b, ok := m.blobs[sha256]; ok {
		b.StoragePath = storagePath
	}
	return nil
}

func (m *mockMetadata) IncrBlobRef(_ context.Context, sha256 string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if b, ok := m.blobs[sha256]; ok {
		b.RefCount++
	}
	return nil
}

func (m *mockMetadata) DecrBlobRef(_ context.Context, sha256 string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.blobs[sha256]
	if !ok {
		return 0, nil
	}
	if b.RefCount > 0 {
		b.RefCount--
	}
	return b.RefCount, nil
}

func (m *mockMetadata) PutFile(_ context.Context, f *FileMetadata) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[f.FileID] = f
	return nil
}

func (m *mockMetadata) GetFile(_ context.Context, id string) (*FileMetadata, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.files[id]
	if !ok {
		return nil, nil
	}
	return f, nil
}

func (m *mockMetadata) GetFileByPath(_ context.Context, namespace, path string) (*FileMetadata, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, f := range m.files {
		if f.Namespace == namespace && f.Path == path {
			return f, nil
		}
	}
	return nil, nil
}


func (m *mockMetadata) ListChildrenPage(_ context.Context, parentID string, search string, page, perPage int, sortBy, sortOrder string) ([]*FileMetadata, int, error) {
	all, _ := m.ListChildren(context.Background(), parentID, search)
	total := len(all)
	start := (page - 1) * perPage
	if start < 0 { start = 0 }
	if start >= total { return nil, total, nil }
	end := start + perPage
	if end > total { end = total }
	return all[start:end], total, nil
}

func (m *mockMetadata) ListRootPage(_ context.Context, namespace string, search string, page, perPage int, sortBy, sortOrder string) ([]*FileMetadata, int, error) {
	all, _ := m.ListRoot(context.Background(), namespace, search)
	total := len(all)
	start := (page - 1) * perPage
	if start < 0 { start = 0 }
	if start >= total { return nil, total, nil }
	end := start + perPage
	if end > total { end = total }
	return all[start:end], total, nil
}

func (m *mockMetadata) ListChildren(_ context.Context, parentID string, search string) ([]*FileMetadata, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var children []*FileMetadata
	for _, f := range m.files {
		if f.ParentID == parentID {
			if search == "" || containsIgnoreCase(f.Name, search) {
				children = append(children, f)
			}
		}
	}
	return children, nil
}

func (m *mockMetadata) DeleteFile(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteFileCount++
	if m.failAfterDeleteFile > 0 && m.deleteFileCount > m.failAfterDeleteFile {
		return fmt.Errorf("mock 失败注入：第 %d 次 DeleteFile 超过 failAfter=%d", m.deleteFileCount, m.failAfterDeleteFile)
	}
	delete(m.files, id)
	return nil
}

// SetFailAfterDeleteFile 配置 mock：第 N 次 DeleteFile 调用后返回错误。
// 0 = 不注入失败。
func (m *mockMetadata) SetFailAfterDeleteFile(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failAfterDeleteFile = n
	m.deleteFileCount = 0
}

func (m *mockMetadata) ListFilesByBlob(_ context.Context, sha256 string) ([]*FileMetadata, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var refs []*FileMetadata
	for _, f := range m.files {
		if f.SHA256 == sha256 {
			refs = append(refs, f)
		}
	}
	return refs, nil
}

func (m *mockMetadata) RenameFile(_ context.Context, fileID, newName, newPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if f, ok := m.files[fileID]; ok {
		f.Name = newName
		f.Path = newPath
	}
	return nil
}

func (m *mockMetadata) SetFileTags(_ context.Context, fileID string, tags []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if f, ok := m.files[fileID]; ok {
		f.Tags = append([]string{}, tags...)
	}
	return nil
}

func (m *mockMetadata) GetFileTags(_ context.Context, fileID string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if f, ok := m.files[fileID]; ok {
		return append([]string{}, f.Tags...), nil
	}
	return nil, nil
}

func (m *mockMetadata) DeleteFileTags(_ context.Context, fileID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if f, ok := m.files[fileID]; ok {
		f.Tags = nil
	}
	return nil
}

func (m *mockMetadata) ReparentFile(_ context.Context, fileID string, parentID *string, path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if f, ok := m.files[fileID]; ok {
		f.Path = path
		if parentID == nil {
			f.ParentID = ""
		} else {
			f.ParentID = *parentID
		}
	}
	return nil
}

func (m *mockMetadata) UpdateFileParent(_ context.Context, fileID string, parentID *string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if f, ok := m.files[fileID]; ok {
		if parentID == nil {
			f.ParentID = ""
		} else {
			f.ParentID = *parentID
		}
	}
	return nil
}

func (m *mockMetadata) ListRoot(_ context.Context, namespace string, search string) ([]*FileMetadata, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var roots []*FileMetadata
	for _, f := range m.files {
		if f.ParentID == "" && f.Namespace == namespace {
			if search == "" || containsIgnoreCase(f.Name, search) {
				roots = append(roots, f)
			}
		}
	}
	return roots, nil
}

func (m *mockMetadata) ListAllBlobs(_ context.Context) ([]*ContentBlob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var blobs []*ContentBlob
	for _, b := range m.blobs {
		blobs = append(blobs, b)
	}
	return blobs, nil
}

func (m *mockMetadata) ListAllFiles(_ context.Context) ([]*FileMetadata, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var files []*FileMetadata
	for _, f := range m.files {
		files = append(files, f)
	}
	return files, nil
}

// ===== AdminStore =====

func (m *mockMetadata) WriteAuditLog(_ context.Context, _ *AuditLogEntry) error { return nil }
func (m *mockMetadata) ListAuditLogs(_ context.Context, _, _ int) ([]*AuditLogEntry, int, error) { return nil, 0, nil }
func (m *mockMetadata) AdminCountFiles(_ context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.files), nil
}
func (m *mockMetadata) AdminCountBlobs(_ context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.blobs), nil
}
func (m *mockMetadata) AdminTotalBlobSize(_ context.Context) (int64, error) { return 0, nil }

func (m *mockMetadata) hasBlob(sha256 string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.blobs[sha256]
	return ok
}

func (m *mockMetadata) hasFile(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.files[id]
	return ok
}

// ===== Mock Compressor =====

type mockCompressor struct{}

func newMockCompressor() *mockCompressor { return &mockCompressor{} }

func (m *mockCompressor) Decompress(_ context.Context, r io.Reader, _ CompressionFormat) (io.ReadCloser, error) {
	return io.NopCloser(r), nil
}

func (m *mockCompressor) NewArchiveWriter(_ context.Context, w io.Writer, _ CompressionFormat) (ArchiveWriter, error) {
	return &mockArchiveWriter{w: w}, nil
}

type mockArchiveWriter struct {
	w io.Writer
}

func (m *mockArchiveWriter) AddFile(_ context.Context, name string, size int64, content io.Reader) error {
	_, err := io.Copy(m.w, content)
	return err
}

func (m *mockArchiveWriter) Close() error { return nil }

// ===== Mock Hasher =====

type mockHasher struct{}

func newMockHasher() *mockHasher { return &mockHasher{} }

func (m *mockHasher) Sum(_ context.Context, r io.Reader) (string, int64, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", 0, err
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:]), int64(len(data)), nil
}

func (m *mockHasher) TeeReader(r io.Reader) (io.Reader, HashAccumulator) {
	acc := NewMockHashAccumulator()
	tee := io.TeeReader(r, acc)
	return tee, acc
}

// MockHashAccumulator 模拟 HashAccumulator
type MockHashAccumulator struct {
	data []byte
}

func NewMockHashAccumulator() *MockHashAccumulator {
	return &MockHashAccumulator{}
}

func (m *MockHashAccumulator) Write(p []byte) (int, error) {
	m.data = append(m.data, p...)
	return len(p), nil
}

func (m *MockHashAccumulator) SumHex() string {
	h := sha256.Sum256(m.data)
	return hex.EncodeToString(h[:])
}

func (m *MockHashAccumulator) N() int64 {
	return int64(len(m.data))
}

// ===== Mock WorkerPool =====

type mockWorkerPool struct {
	runImmediate bool
}

func newMockWorkerPool() *mockWorkerPool {
	return &mockWorkerPool{runImmediate: true}
}

func (m *mockWorkerPool) Submit(ctx context.Context, task func()) error {
	if m.runImmediate {
		task()
	}
	return nil
}

func (m *mockWorkerPool) Stats() WorkerStats {
	return WorkerStats{Capacity: 4, Available: 4}
}

// compile-time checks
var (
	_ Storage       = (*mockStorage)(nil)
	_ Metadata      = (*mockMetadata)(nil)
	_ Compressor    = (*mockCompressor)(nil)
	_ Hasher        = (*mockHasher)(nil)
	_ WorkerPool    = (*mockWorkerPool)(nil)
	_ ArchiveWriter = (*mockArchiveWriter)(nil)
)

// containsIgnoreCase 检查 s 是否包含 substr（大小写不敏感）
func containsIgnoreCase(s, substr string) bool {
	if len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			sc, tc := s[i+j], substr[j]
			if sc >= 'A' && sc <= 'Z' {
				sc += 32
			}
			if tc >= 'A' && tc <= 'Z' {
				tc += 32
			}
			if sc != tc {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func (m *mockStorage) HealthCheck(_ context.Context) error { return nil }
func (m *mockMetadata) HealthCheck(_ context.Context) error { return nil }

func (m *mockMetadata) CreateShare(_ context.Context, _ string, _ *ShareEntry) error { return nil }
func (m *mockMetadata) GetShare(_ context.Context, _ string) (*ShareEntry, error)    { return nil, nil }
func (m *mockMetadata) IncrDownloads(_ context.Context, _ string) error              { return nil }
