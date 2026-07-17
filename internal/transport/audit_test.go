package transport

import (
	"context"
	"errors"
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
