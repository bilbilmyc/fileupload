package transport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

type captureAuditLogger struct {
	entries []*domain.AuditLogEntry
	err     error
}

func (l *captureAuditLogger) WriteAuditLog(_ context.Context, entry *domain.AuditLogEntry) error {
	l.entries = append(l.entries, entry)
	return l.err
}

func TestWriteAuditRecordsAuthenticatedActor(t *testing.T) {
	logger := &captureAuditLogger{}
	ctx := context.WithValue(context.Background(), ctxKeyAuthClaims, &domain.AuthClaims{UserID: "user-42"})

	writeAudit(logger, ctx, "download", "file", "file-7", "team-a", "single file download")

	if len(logger.entries) != 1 {
		t.Fatalf("entry count = %d, want 1", len(logger.entries))
	}
	entry := logger.entries[0]
	if entry.UserID != "user-42" || entry.Action != "download" || entry.TargetID != "file-7" || entry.Namespace != "team-a" {
		t.Fatalf("unexpected audit entry: %#v", entry)
	}
}

func TestWriteAuditUsesAnonymousAndNeverPropagatesFailure(t *testing.T) {
	logger := &captureAuditLogger{err: errors.New("storage unavailable")}

	writeAudit(logger, context.Background(), "preview", "file", "file-9", "default", "inline file preview")

	if len(logger.entries) != 1 {
		t.Fatalf("entry count = %d, want 1", len(logger.entries))
	}
	if logger.entries[0].UserID != "anonymous" {
		t.Fatalf("user id = %q, want anonymous", logger.entries[0].UserID)
	}
}

func TestAuditMiddlewareRecordsMutationMetadata(t *testing.T) {
	logger := &captureAuditLogger{}
	mw := NewMiddleware().WithAuditLogger(logger)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/uploads/{id}/finalize", func(w http.ResponseWriter, r *http.Request) {
		updateAuditActor(r.Context(), "user-7")
		updateAuditNamespace(r.Context(), "team-a")
		w.WriteHeader(http.StatusCreated)
	})
	handler := mw.RequestID(mw.Audit(mux))

	req := httptest.NewRequest(http.MethodPost, "/v1/uploads/session-9/finalize?token=must-not-leak", nil)
	req.RemoteAddr = "192.0.2.10:4321"
	req.Header.Set("X-Request-ID", "req-123")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if len(logger.entries) != 1 {
		t.Fatalf("entry count = %d, want 1", len(logger.entries))
	}
	entry := logger.entries[0]
	if entry.Action != "upload_finalize" || entry.TargetType != "session" || entry.TargetID != "session-9" {
		t.Fatalf("unexpected target: %#v", entry)
	}
	if entry.UserID != "user-7" || entry.Namespace != "team-a" {
		t.Fatalf("unexpected actor: %#v", entry)
	}
	var detail auditDetail
	if err := json.Unmarshal([]byte(entry.Detail), &detail); err != nil {
		t.Fatalf("detail is not JSON: %v", err)
	}
	if detail.Route != "/v1/uploads/{id}/finalize" || detail.Status != http.StatusCreated || detail.RequestID != "req-123" || detail.RemoteIP != "192.0.2.10" {
		t.Fatalf("unexpected detail: %#v", detail)
	}
	if detail.Route == req.URL.String() {
		t.Fatal("audit detail must use route pattern instead of URL with query")
	}
}

func TestAuditMiddlewareRecordsDeniedRequestWithoutLeakingPath(t *testing.T) {
	logger := &captureAuditLogger{}
	mw := NewMiddleware().WithAuditLogger(logger)
	handler := mw.RequestID(mw.Audit(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		respondJSON(w, http.StatusForbidden, map[string]string{"error": "denied"})
	})))

	req := httptest.NewRequest(http.MethodGet, "/s/secret-share-token", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if len(logger.entries) != 1 || logger.entries[0].Action != "access_denied" {
		t.Fatalf("unexpected entries: %#v", logger.entries)
	}
	if logger.entries[0].TargetID != "" {
		t.Fatalf("target id leaked: %q", logger.entries[0].TargetID)
	}
	if logger.entries[0].Detail == "" {
		t.Fatal("expected structured detail")
	}
	if json.Valid([]byte(logger.entries[0].Detail)) == false {
		t.Fatalf("invalid detail JSON: %q", logger.entries[0].Detail)
	}
	if contains := stringContains(logger.entries[0].Detail, "secret-share-token"); contains {
		t.Fatalf("secret path leaked in audit detail: %s", logger.entries[0].Detail)
	}
}

func TestAuditMiddlewareSkipsReadOnlyListing(t *testing.T) {
	logger := &captureAuditLogger{}
	mw := NewMiddleware().WithAuditLogger(logger)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/ls", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	mw.Audit(mux).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/ls", nil))
	if len(logger.entries) != 0 {
		t.Fatalf("read-only listing generated audit entries: %#v", logger.entries)
	}
}

func stringContains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
