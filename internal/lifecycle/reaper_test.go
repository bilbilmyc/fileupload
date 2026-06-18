package lifecycle

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

// ===== In-memory mock Metadata for lifecycle tests =====

type mockMeta struct {
	sessions   map[string]*domain.UploadSession
	blobs      map[string]*domain.ContentBlob
	files      map[string]*domain.FileMetadata
}

func newMockMeta() *mockMeta {
	return &mockMeta{
		sessions: make(map[string]*domain.UploadSession),
		blobs:    make(map[string]*domain.ContentBlob),
		files:    make(map[string]*domain.FileMetadata),
	}
}

func (m *mockMeta) CreateSession(_ context.Context, s *domain.UploadSession) error {
	m.sessions[s.SessionID] = s
	return nil
}
func (m *mockMeta) GetSession(_ context.Context, id string) (*domain.UploadSession, error) {
	s, ok := m.sessions[id]
	if !ok {
		return nil, nil
	}
	return s, nil
}
func (m *mockMeta) UpdateOffset(_ context.Context, _ string, _ int, _ string, _ int64) error { return nil }
func (m *mockMeta) ListChunks(_ context.Context, _ string) ([]domain.ChunkInfo, error) { return nil, nil }
func (m *mockMeta) DeleteSession(_ context.Context, id string) error {
	delete(m.sessions, id)
	return nil
}
func (m *mockMeta) TouchSession(_ context.Context, _ string, _ time.Duration) error { return nil }
func (m *mockMeta) ListExpiredSessions(_ context.Context) ([]string, error) {
	var expired []string
	for id, s := range m.sessions {
		if s.ExpireAt.Before(time.Now()) && s.Status != "completed" && s.Status != "finalizing" {
			expired = append(expired, id)
		}
	}
	return expired, nil
}
func (m *mockMeta) GetBlobBySha(_ context.Context, sha string) (*domain.ContentBlob, error) {
	b, ok := m.blobs[sha]
	if !ok {
		return nil, nil
	}
	return b, nil
}
func (m *mockMeta) PutBlob(_ context.Context, b *domain.ContentBlob) error {
	m.blobs[b.SHA256] = b
	return nil
}
func (m *mockMeta) IncrBlobRef(_ context.Context, _ string) error { return nil }
func (m *mockMeta) DecrBlobRef(_ context.Context, _ string) (int, error) { return 0, nil }
func (m *mockMeta) PutFile(_ context.Context, f *domain.FileMetadata) error {
	m.files[f.FileID] = f
	return nil
}
func (m *mockMeta) GetFile(_ context.Context, id string) (*domain.FileMetadata, error) {
	f, ok := m.files[id]
	if !ok {
		return nil, nil
	}
	return f, nil
}
func (m *mockMeta) GetFileByPath(_ context.Context, _, _ string) (*domain.FileMetadata, error) { return nil, nil }
func (m *mockMeta) ListChildren(_ context.Context, _ string) ([]*domain.FileMetadata, error) { return nil, nil }
func (m *mockMeta) DeleteFile(_ context.Context, _ string) error { return nil }
func (m *mockMeta) ListFilesByBlob(_ context.Context, sha string) ([]*domain.FileMetadata, error) {
	var refs []*domain.FileMetadata
	for _, f := range m.files {
		if f.SHA256 == sha {
			refs = append(refs, f)
		}
	}
	return refs, nil
}
func (m *mockMeta) ListRoot(_ context.Context, _ string) ([]*domain.FileMetadata, error) { return nil, nil }
func (m *mockMeta) ListAllBlobs(_ context.Context) ([]*domain.ContentBlob, error) {
	var blobs []*domain.ContentBlob
	for _, b := range m.blobs {
		blobs = append(blobs, b)
	}
	return blobs, nil
}
func (m *mockMeta) ListAllFiles(_ context.Context) ([]*domain.FileMetadata, error) {
	var files []*domain.FileMetadata
	for _, f := range m.files {
		files = append(files, f)
	}
	return files, nil
}

// ===== In-memory mock Storage =====

type mockStorage struct {
	files map[string][]byte
}

func newMockStorage() *mockStorage {
	return &mockStorage{files: make(map[string][]byte)}
}
func (m *mockStorage) Write(_ context.Context, path string, r io.Reader) (int64, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return 0, err
	}
	m.files[path] = data
	return int64(len(data)), nil
}
func (m *mockStorage) Open(_ context.Context, _ string, _, _ int64) (io.ReadCloser, error) {
	return nil, nil
}
func (m *mockStorage) Delete(_ context.Context, path string) error {
	delete(m.files, path)
	return nil
}
func (m *mockStorage) Stat(_ context.Context, path string) (int64, bool, error) {
	data, ok := m.files[path]
	if !ok {
		return 0, false, nil
	}
	return int64(len(data)), true, nil
}

// ===== Tests =====

func TestNewSessionReaper(t *testing.T) {
	r := NewSessionReaper(nil, nil, "/tmp", time.Minute)
	if r == nil {
		t.Fatal("NewSessionReaper returned nil")
	}
	if r.interval != time.Minute {
		t.Errorf("interval = %v, want 1m", r.interval)
	}
	r.Stop()
}

func TestSessionReaper_DefaultInterval(t *testing.T) {
	r := NewSessionReaper(nil, nil, "/tmp", 0)
	if r.interval != time.Minute {
		t.Errorf("default interval = %v, want 1m", r.interval)
	}
}

func TestSessionReaper_StartStop(t *testing.T) {
	dir := t.TempDir()
	r := NewSessionReaper(nil, nil, dir, 100*time.Millisecond)
	r.Start()
	time.Sleep(250 * time.Millisecond)
	r.Stop()
}

func TestReap_CleansExpiredSessions(t *testing.T) {
	meta := newMockMeta()
	dir := t.TempDir()

	// Create an expired session (expire in past)
	sessionID := "expired-session-1"
	meta.CreateSession(context.Background(), &domain.UploadSession{
		SessionID: sessionID,
		ExpireAt:  time.Now().Add(-1 * time.Hour),
		Status:    "active",
	})

	// Create temp chunk dir for this session
	sessionDir := filepath.Join(dir, sessionID)
	os.MkdirAll(sessionDir, 0755)
	os.WriteFile(filepath.Join(sessionDir, "0.part"), []byte("data"), 0644)
	os.WriteFile(filepath.Join(sessionDir, "1.part"), []byte("more"), 0644)

	r := NewSessionReaper(meta, nil, dir, time.Minute)
	r.reap(context.Background())

	// Session should be deleted from meta
	session, _ := meta.GetSession(context.Background(), sessionID)
	if session != nil {
		t.Error("expected session to be deleted after reap")
	}

	// Temp dir should be cleaned up
	if _, err := os.Stat(sessionDir); !os.IsNotExist(err) {
		t.Errorf("expected temp dir %s to be removed", sessionDir)
	}
}

func TestReap_KeepsActiveSessions(t *testing.T) {
	meta := newMockMeta()
	dir := t.TempDir()

	// Create an active session (expire in future)
	sessionID := "active-session-1"
	meta.CreateSession(context.Background(), &domain.UploadSession{
		SessionID: sessionID,
		ExpireAt:  time.Now().Add(1 * time.Hour),
		Status:    "active",
	})

	sessionDir := filepath.Join(dir, sessionID)
	os.MkdirAll(sessionDir, 0755)
	os.WriteFile(filepath.Join(sessionDir, "0.part"), []byte("data"), 0644)

	r := NewSessionReaper(meta, nil, dir, time.Minute)
	r.reap(context.Background())

	// Session should still exist
	session, _ := meta.GetSession(context.Background(), sessionID)
	if session == nil {
		t.Error("expected active session to be preserved")
	}
	// Temp dir should not be removed
	if _, err := os.Stat(sessionDir); os.IsNotExist(err) {
		t.Error("expected active session temp dir to be preserved")
	}
}

func TestReap_HandlesListError(t *testing.T) {
	// reap with nil meta should not panic
	r := NewSessionReaper(nil, nil, "/tmp", time.Minute)
	r.reap(context.Background()) // should not panic
}

func TestCleanupOrphanParts_RemovesOrphanDirs(t *testing.T) {
	meta := newMockMeta()
	dir := t.TempDir()

	// Create orphan dir (no session in meta)
	orphanDir := filepath.Join(dir, "orphan-session")
	os.MkdirAll(orphanDir, 0755)
	os.WriteFile(filepath.Join(orphanDir, "0.part"), []byte("x"), 0644)

	r := NewSessionReaper(meta, nil, dir, time.Minute)
	r.reap(context.Background())

	if _, err := os.Stat(orphanDir); !os.IsNotExist(err) {
		t.Errorf("expected orphan dir %s to be removed", orphanDir)
	}
}

func TestCleanupOrphanParts_KeepsActiveDirs(t *testing.T) {
	meta := newMockMeta()
	dir := t.TempDir()

	sessionID := "alive-session"
	meta.CreateSession(context.Background(), &domain.UploadSession{
		SessionID: sessionID,
		ExpireAt:  time.Now().Add(1 * time.Hour),
		Status:    "active",
	})

	sessionDir := filepath.Join(dir, sessionID)
	os.MkdirAll(sessionDir, 0755)
	os.WriteFile(filepath.Join(sessionDir, "0.part"), []byte("data"), 0644)

	r := NewSessionReaper(meta, nil, dir, time.Minute)
	r.reap(context.Background())

	if _, err := os.Stat(sessionDir); os.IsNotExist(err) {
		t.Error("expected active session dir to be preserved")
	}
}

// ===== ConsistencyScanner tests =====

func TestNewConsistencyScanner(t *testing.T) {
	s := NewConsistencyScanner(nil, nil, "/tmp/data", "/tmp/tmp")
	if s == nil {
		t.Fatal("NewConsistencyScanner returned nil")
	}
}

func TestScanner_ScanOrphanParts(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "orphan1.part"), []byte("x"), 0644)
	os.WriteFile(filepath.Join(dir, "orphan2.part"), []byte("x"), 0644)

	s := NewConsistencyScanner(nil, nil, "/tmp/data", dir)
	result, err := s.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan error = %v", err)
	}
	report := result.(*ScannerReport)
	if report.OrphanParts != 2 {
		t.Errorf("OrphanParts = %d, want 2", report.OrphanParts)
	}
}

func TestScanner_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	s := NewConsistencyScanner(nil, nil, "/tmp/data", dir)
	result, err := s.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan error = %v", err)
	}
	report := result.(*ScannerReport)
	if report.OrphanParts != 0 {
		t.Errorf("OrphanParts = %d, want 0", report.OrphanParts)
	}
}

func TestScanner_ScanOrphanFiles(t *testing.T) {
	meta := newMockMeta()
	dataDir := t.TempDir()
	dir := t.TempDir()

	// Create a file in data dir that has no metadata record
	nsDir := filepath.Join(dataDir, "default")
	os.MkdirAll(nsDir, 0755)
	os.WriteFile(filepath.Join(nsDir, "orphan-file-id"), []byte("content"), 0644)

	s := NewConsistencyScanner(meta, newMockStorage(), dataDir, dir)
	result, err := s.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan error = %v", err)
	}
	report := result.(*ScannerReport)
	if len(report.OrphanFiles) != 1 {
		t.Errorf("OrphanFiles len = %d, want 1", len(report.OrphanFiles))
	}
}

func TestScanner_ScanOrphanFiles_WithExistingRecord(t *testing.T) {
	meta := newMockMeta()
	dataDir := t.TempDir()
	dir := t.TempDir()

	// Create a file AND a metadata record — should not be orphan
	nsDir := filepath.Join(dataDir, "default")
	os.MkdirAll(nsDir, 0755)
	fileID := "known-file-id"
	os.WriteFile(filepath.Join(nsDir, fileID), []byte("content"), 0644)
	meta.PutFile(context.Background(), &domain.FileMetadata{
		FileID: fileID,
		SHA256: "abc",
		Name:   "test.txt",
		Size:   7,
	})

	s := NewConsistencyScanner(meta, newMockStorage(), dataDir, dir)
	result, err := s.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan error = %v", err)
	}
	report := result.(*ScannerReport)
	if len(report.OrphanFiles) != 0 {
		t.Errorf("OrphanFiles len = %d, want 0 (file has metadata)", len(report.OrphanFiles))
	}
}

func TestScanner_ScanMetadataOrphans(t *testing.T) {
	meta := newMockMeta()
	storage := newMockStorage()
	dir := t.TempDir()

	// Create blob pointing to a storage path that doesn't exist
	meta.PutBlob(context.Background(), &domain.ContentBlob{
		SHA256:      "missing-sha",
		StoragePath: "default/nonexistent-file",
		Size:        10,
	})

	s := NewConsistencyScanner(meta, storage, "/tmp/data", dir)
	result, err := s.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan error = %v", err)
	}
	report := result.(*ScannerReport)
	if report.MetadataOrphans != 1 {
		t.Errorf("MetadataOrphans = %d, want 1", report.MetadataOrphans)
	}
}

func TestScanner_ScanMetadataOrphans_Clean(t *testing.T) {
	meta := newMockMeta()
	storage := newMockStorage()
	dir := t.TempDir()

	// Create blob with a storage file that exists
	storage.Write(context.Background(), "default/existing-file", bytes.NewReader([]byte{}))
	meta.PutBlob(context.Background(), &domain.ContentBlob{
		SHA256:      "present-sha",
		StoragePath: "default/existing-file",
		Size:        0,
	})

	s := NewConsistencyScanner(meta, storage, "/tmp/data", dir)
	result, err := s.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan error = %v", err)
	}
	report := result.(*ScannerReport)
	if report.MetadataOrphans != 0 {
		t.Errorf("MetadataOrphans = %d, want 0", report.MetadataOrphans)
	}
}

func TestScanner_ScanRefCount(t *testing.T) {
	meta := newMockMeta()
	storage := newMockStorage()
	dir := t.TempDir()

	// Create blob with ref_count=2 but only 1 file referencing it
	storage.Write(context.Background(), "default/drift-file", bytes.NewReader([]byte("d")))
	meta.PutBlob(context.Background(), &domain.ContentBlob{
		SHA256: "drift-sha", StoragePath: "default/drift-file",
		RefCount: 2,
		Size:     1,
	})
	meta.PutFile(context.Background(), &domain.FileMetadata{
		FileID: "file-1",
		SHA256: "drift-sha",
		Name:   "a.txt",
	})

	s := NewConsistencyScanner(meta, storage, "/tmp/data", dir)
	result, err := s.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan error = %v", err)
	}
	report := result.(*ScannerReport)
	if report.RefCountFixes != 1 {
		t.Errorf("RefCountFixes = %d, want 1", report.RefCountFixes)
	}
}

func TestScanner_ScanRefCount_Clean(t *testing.T) {
	meta := newMockMeta()
	storage := newMockStorage()
	dir := t.TempDir()

	storage.Write(context.Background(), "default/clean-file", bytes.NewReader([]byte("c")))
	meta.PutBlob(context.Background(), &domain.ContentBlob{
		SHA256: "clean-sha", StoragePath: "default/clean-file",
		RefCount: 2,
		Size:     1,
	})
	meta.PutFile(context.Background(), &domain.FileMetadata{
		FileID: "file-1", SHA256: "clean-sha", Name: "a.txt",
	})
	meta.PutFile(context.Background(), &domain.FileMetadata{
		FileID: "file-2", SHA256: "clean-sha", Name: "b.txt",
	})

	s := NewConsistencyScanner(meta, newMockStorage(), "/tmp/data", dir)
	result, err := s.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan error = %v", err)
	}
	report := result.(*ScannerReport)
	if report.RefCountFixes != 0 {
		t.Errorf("RefCountFixes = %d, want 0", report.RefCountFixes)
	}
}

func TestScanner_ScanFull(t *testing.T) {
	meta := newMockMeta()
	storage := newMockStorage()
	dataDir := t.TempDir()
	tempDir := t.TempDir()

	// Orphan parts in temp dir
	os.WriteFile(filepath.Join(tempDir, "lonely.part"), []byte("x"), 0644)

	// Orphan file on disk (no metadata record)
	nsDir := filepath.Join(dataDir, "default")
	os.MkdirAll(nsDir, 0755)
	os.WriteFile(filepath.Join(nsDir, "orphan-file"), []byte("data"), 0644)

	// Metadata orphan (blob but no file)
	meta.PutBlob(context.Background(), &domain.ContentBlob{
		SHA256: "meta-orphan", StoragePath: "default/missing", Size: 5,
	})

	// Ref count drift — blob has valid storage, so no metadata orphan
	storage.Write(context.Background(), "default/drift-file", bytes.NewReader([]byte("x")))
	meta.PutBlob(context.Background(), &domain.ContentBlob{
		SHA256: "drift", StoragePath: "default/drift-file", RefCount: 3, Size: 1,
	})
	meta.PutFile(context.Background(), &domain.FileMetadata{
		FileID: "f1", SHA256: "drift", Name: "f1.txt",
	})

	s := NewConsistencyScanner(meta, storage, dataDir, tempDir)
	result, err := s.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan error = %v", err)
	}
	report := result.(*ScannerReport)

	if report.OrphanParts != 1 {
		t.Errorf("OrphanParts = %d, want 1", report.OrphanParts)
	}
	if len(report.OrphanFiles) != 1 {
		t.Errorf("OrphanFiles len = %d, want 1", len(report.OrphanFiles))
	}
	if report.MetadataOrphans != 1 {
		t.Errorf("MetadataOrphans = %d, want 1", report.MetadataOrphans)
	}
	if report.RefCountFixes != 1 {
		t.Errorf("RefCountFixes = %d, want 1", report.RefCountFixes)
	}
}
