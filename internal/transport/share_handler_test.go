package transport

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

// mockShareStore implements domain.ShareStore
type mockShareStore struct {
	shares map[string]*domain.ShareEntry
}

func newMockShareStore() *mockShareStore {
	return &mockShareStore{shares: make(map[string]*domain.ShareEntry)}
}
func (m *mockShareStore) CreateShare(_ context.Context, token string, entry *domain.ShareEntry) error {
	m.shares[token] = entry
	return nil
}
func (m *mockShareStore) GetShare(_ context.Context, token string) (*domain.ShareEntry, error) {
	e, ok := m.shares[token]
	if !ok {
		return nil, nil
	}
	return e, nil
}
func (m *mockShareStore) IncrDownloads(_ context.Context, token string) error {
	if e, ok := m.shares[token]; ok {
		e.CurDownloads++
	}
	return nil
}

func TestShareHandler_CreateShare(t *testing.T) {
	store := newMockShareStore()
	shareSvc := domain.NewShareService(store)
	handler := NewShareHandler(shareSvc, nil)

	body, _ := json.Marshal(domain.CreateShareRequest{FileID: "f1"})
	req := httptest.NewRequest("POST", "/v1/share", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.CreateShare(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", w.Code)
	}

	var entry domain.ShareEntry
	json.NewDecoder(w.Body).Decode(&entry)
	if entry.Token == "" {
		t.Error("Token is empty")
	}
	if entry.FileID != "f1" {
		t.Errorf("FileID = %s, want f1", entry.FileID)
	}
}

func TestShareHandler_CreateShare_EmptyFileID(t *testing.T) {
	store := newMockShareStore()
	handler := NewShareHandler(domain.NewShareService(store), nil)

	body, _ := json.Marshal(domain.CreateShareRequest{FileID: ""})
	req := httptest.NewRequest("POST", "/v1/share", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.CreateShare(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestShareHandler_CreateShare_WithPassword(t *testing.T) {
	store := newMockShareStore()
	handler := NewShareHandler(domain.NewShareService(store), nil)

	body, _ := json.Marshal(domain.CreateShareRequest{FileID: "f1", Password: "secret123"})
	req := httptest.NewRequest("POST", "/v1/share", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.CreateShare(w, req)

	var entry domain.ShareEntry
	json.NewDecoder(w.Body).Decode(&entry)
	if entry.PasswordHash != "" {
		t.Error("PasswordHash should not be returned in JSON")
	}
}

func TestShareHandler_AccessShare_NotFound(t *testing.T) {
	store := newMockShareStore()
	handler := NewShareHandler(domain.NewShareService(store), nil)

	req := httptest.NewRequest("GET", "/s/no-such-token", nil)
	w := httptest.NewRecorder()
	handler.AccessShare(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestShareHandler_AccessShare_PasswordRequired(t *testing.T) {
	store := newMockShareStore()
	handler := NewShareHandler(domain.NewShareService(store), nil)

	// Create share with password
	h := sha256.Sum256([]byte("pw"))
	store.CreateShare(context.Background(), "s-test", &domain.ShareEntry{
		Token: "s-test", FileID: "f1", PasswordHash: hex.EncodeToString(h[:]), Namespace: "demo",
	})

	// Access without password
	req := httptest.NewRequest("GET", "/s/s-test", nil)
	w := httptest.NewRecorder()
	handler.AccessShare(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestShareHandler_AccessShare_WithPassword(t *testing.T) {
	store := newMockShareStore()
	handler := NewShareHandler(domain.NewShareService(store), nil)

	h := sha256.Sum256([]byte("pw"))
	store.CreateShare(context.Background(), "s-test", &domain.ShareEntry{
		Token: "s-test", FileID: "f1", PasswordHash: hex.EncodeToString(h[:]), Namespace: "demo",
	})

	req := httptest.NewRequest("GET", "/s/s-test", nil)
	req.Header.Set("X-Share-Password", "pw")
	w := httptest.NewRecorder()
	handler.AccessShare(w, req)

	// Should redirect to file download
	if w.Code != http.StatusFound {
		t.Errorf("status = %d, want 302", w.Code)
	}
}

func TestShareHandler_AccessShare_MaxDownloads(t *testing.T) {
	store := newMockShareStore()
	handler := NewShareHandler(domain.NewShareService(store), nil)

	store.CreateShare(context.Background(), "s-limited", &domain.ShareEntry{
		Token: "s-limited", FileID: "f1", MaxDownloads: 1, CurDownloads: 1, Namespace: "demo",
	})

	req := httptest.NewRequest("GET", "/s/s-limited", nil)
	w := httptest.NewRecorder()
	handler.AccessShare(w, req)

	if w.Code != http.StatusGone {
		t.Errorf("status = %d, want 410", w.Code)
	}
}
