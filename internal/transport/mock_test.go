package transport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"sync"
	"time"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

// mockMeta implements domain.Metadata
type mockMeta struct {
	sessions map[string]*domain.UploadSession
	chunks   map[string][]domain.ChunkInfo
	blobs    map[string]*domain.ContentBlob
	files    map[string]*domain.FileMetadata
}

func newMockMeta() *mockMeta {
	return &mockMeta{
		sessions: make(map[string]*domain.UploadSession),
		chunks:   make(map[string][]domain.ChunkInfo),
		blobs:    make(map[string]*domain.ContentBlob),
		files:    make(map[string]*domain.FileMetadata),
	}
}

func (m *mockMeta) CreateSession(_ context.Context, s *domain.UploadSession) error {
	m.sessions[s.SessionID] = s; return nil }
func (m *mockMeta) GetSession(_ context.Context, id string) (*domain.UploadSession, error) {
	s, ok := m.sessions[id]; if !ok { return nil, nil }; return s, nil }
func (m *mockMeta) UpdateOffset(_ context.Context, id string, idx int, sha string, add int64) error {
	m.chunks[id] = append(m.chunks[id], domain.ChunkInfo{Index: idx, SHA256: sha, Size: add}); return nil }
func (m *mockMeta) ListChunks(_ context.Context, id string) ([]domain.ChunkInfo, error) {
	return append([]domain.ChunkInfo{}, m.chunks[id]...), nil }
func (m *mockMeta) DeleteSession(_ context.Context, id string) error {
	delete(m.sessions, id); delete(m.chunks, id); return nil }
func (m *mockMeta) TouchSession(_ context.Context, id string, ttl time.Duration) error {
	if s, ok := m.sessions[id]; ok { s.ExpireAt = time.Now().Add(ttl) }; return nil }
func (m *mockMeta) ListExpiredSessions(_ context.Context) ([]string, error) { return nil, nil }
func (m *mockMeta) GetBlobBySha(_ context.Context, sha string) (*domain.ContentBlob, error) {
	b, ok := m.blobs[sha]; if !ok { return nil, nil }; return b, nil }
func (m *mockMeta) PutBlob(_ context.Context, b *domain.ContentBlob) error {
	m.blobs[b.SHA256] = b; return nil }
func (m *mockMeta) IncrBlobRef(_ context.Context, sha string) error {
	if b, ok := m.blobs[sha]; ok { b.RefCount++ }; return nil }
func (m *mockMeta) DecrBlobRef(_ context.Context, sha string) (int, error) {
	b, ok := m.blobs[sha]; if !ok { return 0, nil }; if b.RefCount > 0 { b.RefCount-- }; return b.RefCount, nil }
func (m *mockMeta) PutFile(_ context.Context, f *domain.FileMetadata) error {
	m.files[f.FileID] = f; return nil }
func (m *mockMeta) GetFile(_ context.Context, id string) (*domain.FileMetadata, error) {
	f, ok := m.files[id]; if !ok { return nil, nil }; return f, nil }
func (m *mockMeta) GetFileByPath(_ context.Context, ns, path string) (*domain.FileMetadata, error) {
	for _, f := range m.files { if f.Namespace == ns && f.Path == path { return f, nil } }; return nil, nil }
func (m *mockMeta) ListChildren(_ context.Context, pid string) ([]*domain.FileMetadata, error) {
	var c []*domain.FileMetadata; for _, f := range m.files { if f.ParentID == pid { c = append(c, f) } }; return c, nil }
func (m *mockMeta) DeleteFile(_ context.Context, id string) error {
	delete(m.files, id); return nil }
func (m *mockMeta) ListFilesByBlob(_ context.Context, sha string) ([]*domain.FileMetadata, error) {
	var r []*domain.FileMetadata; for _, f := range m.files { if f.SHA256 == sha { r = append(r, f) } }; return r, nil }
func (m *mockMeta) SetFileTags(_ context.Context, fileID string, tags []string) error {
	if f, ok := m.files[fileID]; ok { f.Tags = append([]string{}, tags...) }; return nil }
func (m *mockMeta) GetFileTags(_ context.Context, fileID string) ([]string, error) {
	if f, ok := m.files[fileID]; ok { return append([]string{}, f.Tags...), nil }; return nil, nil }
func (m *mockMeta) DeleteFileTags(_ context.Context, fileID string) error {
	if f, ok := m.files[fileID]; ok { f.Tags = nil }; return nil }
func (m *mockMeta) ReparentFile(_ context.Context, fileID string, parentID *string, path string) error {
	if f, ok := m.files[fileID]; ok {
		f.Path = path
		if parentID == nil { f.ParentID = "" } else { f.ParentID = *parentID }
	}; return nil }
func (m *mockMeta) UpdateFileParent(_ context.Context, fileID string, parentID *string) error {
	if f, ok := m.files[fileID]; ok {
		if parentID == nil { f.ParentID = "" } else { f.ParentID = *parentID }
	}; return nil }
func (m *mockMeta) ListRoot(_ context.Context, ns string) ([]*domain.FileMetadata, error) {
	var r []*domain.FileMetadata; for _, f := range m.files { if f.ParentID == "" && f.Namespace == ns { r = append(r, f) } }; return r, nil }
func (m *mockMeta) ListAllBlobs(_ context.Context) ([]*domain.ContentBlob, error) {
	var b []*domain.ContentBlob; for _, v := range m.blobs { b = append(b, v) }; return b, nil }
func (m *mockMeta) ListAllFiles(_ context.Context) ([]*domain.FileMetadata, error) {
	var f []*domain.FileMetadata; for _, v := range m.files { f = append(f, v) }; return f, nil }

// mockStore implements domain.Storage (thread-safe via sync.Mutex)
type mockStore struct {
	mu    sync.Mutex
	files map[string][]byte
}

func newMockStore() *mockStore { return &mockStore{files: make(map[string][]byte)} }

func (m *mockStore) Write(_ context.Context, path string, r io.Reader) (int64, error) {
	d, e := io.ReadAll(r); if e != nil { return 0, e }
	m.mu.Lock(); m.files[path] = d; m.mu.Unlock()
	return int64(len(d)), nil }
func (m *mockStore) Open(_ context.Context, path string, off, length int64) (io.ReadCloser, error) {
	m.mu.Lock(); d, ok := m.files[path]; m.mu.Unlock()
	if !ok { return nil, domain.ErrNotFound }; s, e := int(off), len(d)
	if length > 0 && s+int(length) < e { e = s + int(length) }; if s >= len(d) { return nil, domain.ErrInvalidArgument }
	return io.NopCloser(bytes.NewReader(d[s:e])), nil }
func (m *mockStore) Delete(_ context.Context, path string) error {
	m.mu.Lock(); delete(m.files, path); m.mu.Unlock(); return nil }
func (m *mockStore) Stat(_ context.Context, path string) (int64, bool, error) {
	m.mu.Lock(); d, ok := m.files[path]; m.mu.Unlock()
	if !ok { return 0, false, nil }; return int64(len(d)), true, nil }
func (m *mockStore) Walk(_ context.Context, fn func(path string, info fs.FileInfo) error) error {
	m.mu.Lock(); defer m.mu.Unlock()
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

// mockCompr implements domain.Compressor
type mockCompr struct{}
func newMockCompr() *mockCompr { return &mockCompr{} }
func (m *mockCompr) Decompress(_ context.Context, r io.Reader, _ domain.CompressionFormat) (io.ReadCloser, error) {
	return io.NopCloser(r), nil }
func (m *mockCompr) NewArchiveWriter(_ context.Context, w io.Writer, _ domain.CompressionFormat) (domain.ArchiveWriter, error) {
	return &mockAW{w: w}, nil }
type mockAW struct{ w io.Writer }
func (m *mockAW) AddFile(_ context.Context, _ string, _ int64, r io.Reader) error {
	_, e := io.Copy(m.w, r); return e }
func (m *mockAW) Close() error { return nil }

// mockHashr implements domain.Hasher
type mockHashr struct{}
func newMockHashr() *mockHashr { return &mockHashr{} }
func (m *mockHashr) Sum(_ context.Context, r io.Reader) (string, int64, error) {
	d, e := io.ReadAll(r); if e != nil { return "", 0, e }
	h := sha256.Sum256(d); return hex.EncodeToString(h[:]), int64(len(d)), nil }
func (m *mockHashr) TeeReader(r io.Reader) (io.Reader, domain.HashAccumulator) {
	acc := &mockAcc{}; return io.TeeReader(r, acc), acc }
type mockAcc struct{ data []byte }
func (m *mockAcc) Write(p []byte) (int, error) { m.data = append(m.data, p...); return len(p), nil }
func (m *mockAcc) SumHex() string { h := sha256.Sum256(m.data); return hex.EncodeToString(h[:]) }
func (m *mockAcc) N() int64 { return int64(len(m.data)) }

// mockWP implements domain.WorkerPool
type mockWP struct{}
func newMockWP() *mockWP { return &mockWP{} }
func (m *mockWP) Submit(_ context.Context, fn func()) error { fn(); return nil }
func (m *mockWP) Stats() domain.WorkerStats { return domain.WorkerStats{Capacity: 4, Available: 4} }

// compile-time interface checks
var (
	_ domain.Metadata    = (*mockMeta)(nil)
	_ domain.Storage     = (*mockStore)(nil)
	_ domain.Compressor  = (*mockCompr)(nil)
	_ domain.Hasher      = (*mockHashr)(nil)
	_ domain.WorkerPool  = (*mockWP)(nil)
)
