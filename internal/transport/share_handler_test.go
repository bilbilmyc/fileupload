package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bilbilmyc/fileupload/internal/domain"
	"golang.org/x/crypto/bcrypt"
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
func (m *mockShareStore) ListShares(_ context.Context, namespace, fileID string) ([]*domain.ShareEntry, error) {
	entries := make([]*domain.ShareEntry, 0)
	for _, entry := range m.shares {
		if entry.Namespace == namespace && (fileID == "" || entry.FileID == fileID) {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func (m *mockShareStore) DeleteShare(_ context.Context, token, namespace string) error {
	entry, ok := m.shares[token]
	if !ok || entry.Namespace != namespace {
		return domain.ErrNotFound
	}
	delete(m.shares, token)
	return nil
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
	hash, _ := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.DefaultCost)
	store.CreateShare(context.Background(), "s-test", &domain.ShareEntry{
		Token: "s-test", FileID: "f1", PasswordHash: string(hash), Namespace: "demo",
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

	hash, _ := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.DefaultCost)
	store.CreateShare(context.Background(), "s-test", &domain.ShareEntry{
		Token: "s-test", FileID: "f1", PasswordHash: string(hash), Namespace: "demo",
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

func TestShareHandler_ListAndRevoke(t *testing.T) {
	store := newMockShareStore()
	store.shares["s-default"] = &domain.ShareEntry{Token: "s-default", FileID: "f1", Namespace: "default"}
	store.shares["s-other"] = &domain.ShareEntry{Token: "s-other", FileID: "f2", Namespace: "other"}
	handler := NewShareHandler(domain.NewShareService(store), nil)

	listReq := httptest.NewRequest(http.MethodGet, "/v1/shares?file_id=f1", nil)
	listW := httptest.NewRecorder()
	handler.ListShares(listW, listReq)
	if listW.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", listW.Code)
	}
	var payload struct {
		Shares []domain.ShareEntry `json:"shares"`
	}
	if err := json.NewDecoder(listW.Body).Decode(&payload); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(payload.Shares) != 1 || payload.Shares[0].Token != "s-default" {
		t.Fatalf("unexpected list result: %#v", payload.Shares)
	}

	revokeReq := httptest.NewRequest(http.MethodDelete, "/v1/shares/s-default", nil)
	revokeReq.SetPathValue("token", "s-default")
	revokeW := httptest.NewRecorder()
	handler.RevokeShare(revokeW, revokeReq)
	if revokeW.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d, want 204", revokeW.Code)
	}
	if _, ok := store.shares["s-default"]; ok {
		t.Fatal("share should have been revoked")
	}
}

func TestShareHandler_AccessShare_HTMLPasswordPage(t *testing.T) {
	store := newMockShareStore()
	handler := NewShareHandler(domain.NewShareService(store), nil)
	hash, _ := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.DefaultCost)
	store.CreateShare(context.Background(), "s-html", &domain.ShareEntry{
		Token: "s-html", FileID: "f1", PasswordHash: string(hash), Namespace: "demo",
	})

	req := httptest.NewRequest(http.MethodGet, "/s/s-html", nil)
	req.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()
	handler.AccessShare(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if !strings.Contains(w.Body.String(), "此分享受密码保护") || !strings.Contains(w.Body.String(), `method="post"`) {
		t.Fatalf("unexpected password page: %s", w.Body.String())
	}
	if store.shares["s-html"].CurDownloads != 0 {
		t.Fatalf("opening password page should not count as a download, got %d", store.shares["s-html"].CurDownloads)
	}
}

func TestShareHandler_AccessShare_HTMLExpiredPage(t *testing.T) {
	store := newMockShareStore()
	handler := NewShareHandler(domain.NewShareService(store), nil)
	store.CreateShare(context.Background(), "s-expired", &domain.ShareEntry{
		Token: "s-expired", FileID: "f1", MaxDownloads: 1, CurDownloads: 1, Namespace: "demo",
	})

	req := httptest.NewRequest(http.MethodGet, "/s/s-expired", nil)
	req.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()
	handler.AccessShare(w, req)

	if w.Code != http.StatusGone {
		t.Fatalf("status = %d, want 410", w.Code)
	}
	if !strings.Contains(w.Body.String(), "此分享链接不可用") || strings.Contains(w.Body.String(), `method="post"`) {
		t.Fatalf("unexpected expired page: %s", w.Body.String())
	}
}

func TestShareHandler_SubmitSharePassword_SetsShortLivedCookie(t *testing.T) {
	store := newMockShareStore()
	handler := NewShareHandler(domain.NewShareService(store), nil)
	hash, _ := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.DefaultCost)
	store.CreateShare(context.Background(), "s-cookie", &domain.ShareEntry{
		Token: "s-cookie", FileID: "f1", PasswordHash: string(hash), Namespace: "demo",
	})

	form := strings.NewReader("password=pw")
	req := httptest.NewRequest(http.MethodPost, "/s/s-cookie", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("token", "s-cookie")
	w := httptest.NewRecorder()
	handler.SubmitSharePassword(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("submit status = %d, want 303", w.Code)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != shareAccessCookieName || !cookies[0].HttpOnly {
		t.Fatalf("unexpected cookies: %#v", cookies)
	}
	if store.shares["s-cookie"].CurDownloads != 0 {
		t.Fatalf("password verification should not count as download, got %d", store.shares["s-cookie"].CurDownloads)
	}

	accessReq := httptest.NewRequest(http.MethodGet, "/s/s-cookie", nil)
	accessReq.AddCookie(cookies[0])
	accessW := httptest.NewRecorder()
	handler.AccessShare(accessW, accessReq)
	if accessW.Code != http.StatusFound {
		t.Fatalf("cookie access status = %d, want 302", accessW.Code)
	}
	if store.shares["s-cookie"].CurDownloads != 1 {
		t.Fatalf("cookie download count = %d, want 1", store.shares["s-cookie"].CurDownloads)
	}
}

func TestShareHandler_SubmitSharePassword_ThrottlesRepeatedFailures(t *testing.T) {
	store := newMockShareStore()
	now := time.Date(2026, time.January, 1, 9, 0, 0, 0, time.UTC)
	limiter := newSharePasswordLimiter(2, time.Minute, func() time.Time { return now })
	audit := &captureAuditLogger{}
	handler := NewShareHandler(domain.NewShareService(store), nil).withPasswordLimiter(limiter).WithAuditLogger(audit)
	hash, _ := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.DefaultCost)
	store.CreateShare(context.Background(), "s-brute", &domain.ShareEntry{
		Token: "s-brute", FileID: "f1", PasswordHash: string(hash), Namespace: "demo",
	})

	submit := func(password string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/s/s-brute", strings.NewReader("password="+password))
		req.Header.Set("Accept", "text/html")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = "198.51.100.10:43123"
		req.SetPathValue("token", "s-brute")
		w := httptest.NewRecorder()
		handler.SubmitSharePassword(w, req)
		return w
	}

	if w := submit("wrong"); w.Code != http.StatusUnauthorized {
		t.Fatalf("first wrong password status = %d, want 401", w.Code)
	}
	second := submit("wrong")
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second wrong password status = %d, want 429", second.Code)
	}
	if got := second.Header().Get("Retry-After"); got != "60" {
		t.Fatalf("Retry-After = %q, want 60", got)
	}
	if !strings.Contains(second.Body.String(), `action="/s/s-brute"`) || !strings.Contains(second.Body.String(), "密码尝试次数过多") {
		t.Fatalf("rate-limited password page does not preserve token or message: %s", second.Body.String())
	}
	if w := submit("pw"); w.Code != http.StatusTooManyRequests {
		t.Fatalf("correct password during cooldown status = %d, want 429", w.Code)
	}

	if len(audit.entries) != 2 {
		t.Fatalf("audit entries = %d, want 2", len(audit.entries))
	}
	if audit.entries[0].Action != "share_password_failed" || audit.entries[1].Action != "share_password_throttled" {
		t.Fatalf("unexpected audit actions: %#v", audit.entries)
	}
	if audit.entries[0].TargetID != "" || audit.entries[1].TargetID != "" {
		t.Fatalf("public share audit must not contain token: %#v", audit.entries)
	}
}

func TestShareHandler_SubmitSharePassword_SuccessResetsFailureBudget(t *testing.T) {
	store := newMockShareStore()
	limiter := newSharePasswordLimiter(2, time.Minute, nil)
	handler := NewShareHandler(domain.NewShareService(store), nil).withPasswordLimiter(limiter)
	hash, _ := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.DefaultCost)
	store.CreateShare(context.Background(), "s-reset", &domain.ShareEntry{
		Token: "s-reset", FileID: "f1", PasswordHash: string(hash), Namespace: "demo",
	})

	submit := func(password string) int {
		req := httptest.NewRequest(http.MethodPost, "/s/s-reset", strings.NewReader("password="+password))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = "198.51.100.10:43123"
		req.SetPathValue("token", "s-reset")
		w := httptest.NewRecorder()
		handler.SubmitSharePassword(w, req)
		return w.Code
	}

	if got := submit("wrong"); got != http.StatusUnauthorized {
		t.Fatalf("first wrong password status = %d, want 401", got)
	}
	if got := submit("pw"); got != http.StatusSeeOther {
		t.Fatalf("successful password status = %d, want 303", got)
	}
	if got := submit("wrong"); got != http.StatusUnauthorized {
		t.Fatalf("wrong password after success status = %d, want 401 instead of throttle", got)
	}
}

func TestShareHandler_AccessShare_ProgrammaticPasswordThrottle(t *testing.T) {
	store := newMockShareStore()
	now := time.Date(2026, time.January, 1, 9, 0, 0, 0, time.UTC)
	handler := NewShareHandler(domain.NewShareService(store), nil).withPasswordLimiter(newSharePasswordLimiter(1, time.Minute, func() time.Time { return now }))
	hash, _ := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.DefaultCost)
	store.CreateShare(context.Background(), "s-api", &domain.ShareEntry{
		Token: "s-api", FileID: "f1", PasswordHash: string(hash), Namespace: "demo",
	})

	req := httptest.NewRequest(http.MethodGet, "/s/s-api", nil)
	req.Header.Set("X-Share-Password", "wrong")
	req.RemoteAddr = "198.51.100.10:43123"
	w := httptest.NewRecorder()
	handler.AccessShare(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("programmatic wrong password status = %d, want 429", w.Code)
	}
	if got := w.Header().Get("Retry-After"); got != "60" {
		t.Fatalf("Retry-After = %q, want 60", got)
	}
	var payload struct {
		Code              string `json:"code"`
		RetryAfterSeconds int    `json:"retry_after_seconds"`
	}
	if err := json.NewDecoder(w.Body).Decode(&payload); err != nil {
		t.Fatalf("decode 429 response: %v", err)
	}
	if payload.Code != "share_password_rate_limited" || payload.RetryAfterSeconds != 60 {
		t.Fatalf("unexpected 429 response: %#v", payload)
	}
}
