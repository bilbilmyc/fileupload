package domain

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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

func (m *mockMetadata) ListChildren(_ context.Context, parentID string) ([]*FileMetadata, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var children []*FileMetadata
	for _, f := range m.files {
		if f.ParentID == parentID {
			children = append(children, f)
		}
	}
	return children, nil
}

func (m *mockMetadata) DeleteFile(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.files, id)
	return nil
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

func (m *mockMetadata) ListRoot(_ context.Context, namespace string) ([]*FileMetadata, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var roots []*FileMetadata
	for _, f := range m.files {
		if f.ParentID == "" && f.Namespace == namespace {
			roots = append(roots, f)
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
