package testutil

import (
	"context"
	"sync"
	"time"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

// MockMetadata implements domain.Metadata (thread-safe)
type MockMetadata struct {
	mu         sync.Mutex
	Sessions   map[string]*domain.UploadSession
	SessionTTL map[string]time.Duration
	Chunks     map[string][]domain.ChunkInfo
	Blobs      map[string]*domain.ContentBlob
	Files      map[string]*domain.FileMetadata
}

// NewMockMetadata 创建 MockMetadata
func NewMockMetadata() *MockMetadata {
	return &MockMetadata{
		Sessions:   make(map[string]*domain.UploadSession),
		SessionTTL: make(map[string]time.Duration),
		Chunks:     make(map[string][]domain.ChunkInfo),
		Blobs:      make(map[string]*domain.ContentBlob),
		Files:      make(map[string]*domain.FileMetadata),
	}
}

// ===== SessionStore =====

func (m *MockMetadata) CreateSession(_ context.Context, s *domain.UploadSession) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Sessions[s.SessionID] = s
	return nil
}

func (m *MockMetadata) GetSession(_ context.Context, id string) (*domain.UploadSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.Sessions[id]
	if !ok {
		return nil, nil
	}
	if s.ExpireAt.Before(time.Now()) && s.Status != domain.SessionCompleted {
		return nil, nil
	}
	return s, nil
}

func (m *MockMetadata) UpdateOffset(_ context.Context, id string, sliceIndex int, sliceSha string, addBytes int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Chunks[id] = append(m.Chunks[id], domain.ChunkInfo{
		Index:  sliceIndex,
		SHA256: sliceSha,
		Size:   addBytes,
	})
	return nil
}

func (m *MockMetadata) ListChunks(_ context.Context, id string) ([]domain.ChunkInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]domain.ChunkInfo{}, m.Chunks[id]...), nil
}

func (m *MockMetadata) DeleteSession(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.Sessions, id)
	delete(m.Chunks, id)
	delete(m.SessionTTL, id)
	return nil
}

func (m *MockMetadata) TouchSession(_ context.Context, id string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SessionTTL[id] = ttl
	if s, ok := m.Sessions[id]; ok {
		s.ExpireAt = time.Now().Add(ttl)
	}
	return nil
}

func (m *MockMetadata) ListExpiredSessions(_ context.Context) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var expired []string
	for id, s := range m.Sessions {
		if s.ExpireAt.Before(time.Now()) && s.Status != domain.SessionFinalizing {
			expired = append(expired, id)
		}
	}
	return expired, nil
}

// ===== BlobStore =====

func (m *MockMetadata) GetBlobBySha(_ context.Context, sha256 string) (*domain.ContentBlob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.Blobs[sha256]
	if !ok {
		return nil, nil
	}
	return b, nil
}

func (m *MockMetadata) PutBlob(_ context.Context, b *domain.ContentBlob) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Blobs[b.SHA256] = b
	return nil
}

func (m *MockMetadata) UpdateBlobStorage(_ context.Context, sha256 string, storagePath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if b, ok := m.Blobs[sha256]; ok {
		b.StoragePath = storagePath
	}
	return nil
}

func (m *MockMetadata) IncrBlobRef(_ context.Context, sha256 string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if b, ok := m.Blobs[sha256]; ok {
		b.RefCount++
	}
	return nil
}

func (m *MockMetadata) DecrBlobRef(_ context.Context, sha256 string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.Blobs[sha256]
	if !ok {
		return 0, nil
	}
	if b.RefCount > 0 {
		b.RefCount--
	}
	return b.RefCount, nil
}

// ===== FileStore =====

func (m *MockMetadata) PutFile(_ context.Context, f *domain.FileMetadata) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Files[f.FileID] = f
	return nil
}

func (m *MockMetadata) GetFile(_ context.Context, id string) (*domain.FileMetadata, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.Files[id]
	if !ok {
		return nil, nil
	}
	return f, nil
}

func (m *MockMetadata) GetFileByPath(_ context.Context, namespace, path string) (*domain.FileMetadata, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, f := range m.Files {
		if f.Namespace == namespace && f.Path == path {
			return f, nil
		}
	}
	return nil, nil
}

func (m *MockMetadata) ListChildren(_ context.Context, parentID string, search string) ([]*domain.FileMetadata, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var children []*domain.FileMetadata
	for _, f := range m.Files {
		if f.ParentID == parentID {
			if search == "" || ContainsIgnoreCase(f.Name, search) {
				children = append(children, f)
			}
		}
	}
	return children, nil
}

func (m *MockMetadata) ListRoot(_ context.Context, namespace string, search string) ([]*domain.FileMetadata, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var roots []*domain.FileMetadata
	for _, f := range m.Files {
		if f.ParentID == "" && f.Namespace == namespace {
			if search == "" || ContainsIgnoreCase(f.Name, search) {
				roots = append(roots, f)
			}
		}
	}
	return roots, nil
}

func (m *MockMetadata) DeleteFile(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.Files, id)
	return nil
}

func (m *MockMetadata) ListFilesByBlob(_ context.Context, sha256 string) ([]*domain.FileMetadata, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var refs []*domain.FileMetadata
	for _, f := range m.Files {
		if f.SHA256 == sha256 {
			refs = append(refs, f)
		}
	}
	return refs, nil
}

func (m *MockMetadata) SetFileTags(_ context.Context, fileID string, tags []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if f, ok := m.Files[fileID]; ok {
		f.Tags = append([]string{}, tags...)
	}
	return nil
}

func (m *MockMetadata) GetFileTags(_ context.Context, fileID string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if f, ok := m.Files[fileID]; ok {
		return append([]string{}, f.Tags...), nil
	}
	return nil, nil
}

func (m *MockMetadata) DeleteFileTags(_ context.Context, fileID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if f, ok := m.Files[fileID]; ok {
		f.Tags = nil
	}
	return nil
}

func (m *MockMetadata) ReparentFile(_ context.Context, fileID string, parentID *string, path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if f, ok := m.Files[fileID]; ok {
		f.Path = path
		if parentID == nil {
			f.ParentID = ""
		} else {
			f.ParentID = *parentID
		}
	}
	return nil
}

func (m *MockMetadata) UpdateFileParent(_ context.Context, fileID string, parentID *string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if f, ok := m.Files[fileID]; ok {
		if parentID == nil {
			f.ParentID = ""
		} else {
			f.ParentID = *parentID
		}
	}
	return nil
}

// ===== AdminStore =====

func (m *MockMetadata) WriteAuditLog(_ context.Context, _ *domain.AuditLogEntry) error { return nil }
func (m *MockMetadata) ListAuditLogs(_ context.Context, _, _ int) ([]*domain.AuditLogEntry, int, error) {
	return nil, 0, nil
}
func (m *MockMetadata) AdminCountFiles(_ context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.Files), nil
}
func (m *MockMetadata) AdminCountBlobs(_ context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.Blobs), nil
}
func (m *MockMetadata) AdminTotalBlobSize(_ context.Context) (int64, error) { return 0, nil }
func (m *MockMetadata) ListAllBlobs(_ context.Context) ([]*domain.ContentBlob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var blobs []*domain.ContentBlob
	for _, b := range m.Blobs {
		blobs = append(blobs, b)
	}
	return blobs, nil
}
func (m *MockMetadata) ListAllFiles(_ context.Context) ([]*domain.FileMetadata, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var files []*domain.FileMetadata
	for _, f := range m.Files {
		files = append(files, f)
	}
	return files, nil
}
