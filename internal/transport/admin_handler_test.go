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

// mockAdminMeta implements the admin metadata interface used by AdminHandler
type mockAdminMeta struct {
	fileCount  int
	blobCount  int
	totalSize  int64
	auditLogs  []*domain.AuditLogEntry
	auditTotal int
	auditErr   error // v0.10.0：模拟 ListAuditLogs 失败
}

func (m *mockAdminMeta) WriteAuditLog(ctx context.Context, entry *domain.AuditLogEntry) error {
	m.auditLogs = append(m.auditLogs, entry)
	return nil
}
func (m *mockAdminMeta) ListAuditLogs(ctx context.Context, page, perPage int) ([]*domain.AuditLogEntry, int, error) {
	if m.auditErr != nil {
		return nil, 0, m.auditErr
	}
	return m.auditLogs, m.auditTotal, nil
}
func (m *mockAdminMeta) AdminCountFiles(ctx context.Context) (int, error) { return m.fileCount, nil }
func (m *mockAdminMeta) AdminCountBlobs(ctx context.Context) (int, error) { return m.blobCount, nil }
func (m *mockAdminMeta) AdminTotalBlobSize(ctx context.Context) (int64, error) {
	return m.totalSize, nil
}

type mockAdminWP struct{}

func (m *mockAdminWP) Stats() domain.WorkerStats {
	return domain.WorkerStats{Capacity: 4, Available: 3}
}

func TestAdminHandler_Status(t *testing.T) {
	meta := &mockAdminMeta{fileCount: 10, blobCount: 5, totalSize: 1024}
	handler := NewAdminHandler(meta, &mockAdminWP{}, "/data", "/tmp", "/db", "sqlite")

	req := httptest.NewRequest("GET", "/v1/admin/status", nil)
	w := httptest.NewRecorder()
	handler.Status(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)

	storage := resp["storage"].(map[string]any)
	if storage["total_files"].(float64) != 10 {
		t.Errorf("total_files = %v, want 10", storage["total_files"])
	}
	if storage["total_blobs"].(float64) != 5 {
		t.Errorf("total_blobs = %v, want 5", storage["total_blobs"])
	}

	pool := resp["worker_pool"].(map[string]any)
	if pool["capacity"].(float64) != 4 {
		t.Errorf("capacity = %v, want 4", pool["capacity"])
	}
	if pool["available"].(float64) != 3 {
		t.Errorf("available = %v, want 3", pool["available"])
	}
}

func TestAdminHandler_AuditLog(t *testing.T) {
	meta := &mockAdminMeta{
		auditLogs: []*domain.AuditLogEntry{
			{ID: 1, Action: "delete", Detail: "test"},
		},
		auditTotal: 1,
	}
	handler := NewAdminHandler(meta, &mockAdminWP{}, "", "", "", "")

	req := httptest.NewRequest("GET", "/v1/admin/audit?page=1&per_page=50", nil)
	w := httptest.NewRecorder()
	handler.AuditLog(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)

	if resp["total"].(float64) != 1 {
		t.Errorf("total = %v, want 1", resp["total"])
	}
	entries := resp["entries"].([]any)
	if len(entries) != 1 {
		t.Errorf("entries count = %d, want 1", len(entries))
	}
}

func TestAdminHandler_AuditLog_Empty(t *testing.T) {
	meta := &mockAdminMeta{auditLogs: []*domain.AuditLogEntry{}, auditTotal: 0}
	handler := NewAdminHandler(meta, &mockAdminWP{}, "", "", "", "")

	req := httptest.NewRequest("GET", "/v1/admin/audit", nil)
	w := httptest.NewRecorder()
	handler.AuditLog(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// v0.10.0 新增：自定义分页
func TestAdminHandler_AuditLog_CustomPagination(t *testing.T) {
	meta := &mockAdminMeta{auditLogs: nil, auditTotal: 0}
	handler := NewAdminHandler(meta, &mockAdminWP{}, "", "", "", "")

	req := httptest.NewRequest("GET", "/v1/admin/audit?page=3&per_page=20", nil)
	w := httptest.NewRecorder()
	handler.AuditLog(w, req)

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["page"].(float64) != 3 {
		t.Errorf("page = %v, want 3", resp["page"])
	}
	if resp["per_page"].(float64) != 20 {
		t.Errorf("per_page = %v, want 20", resp["per_page"])
	}
}

// v0.10.0 新增：非法分页参数 clamp
func TestAdminHandler_AuditLog_InvalidPaginationClamped(t *testing.T) {
	meta := &mockAdminMeta{auditLogs: nil, auditTotal: 0}
	handler := NewAdminHandler(meta, &mockAdminWP{}, "", "", "", "")

	// page=0 应 clamp 到 1；per_page=200 应 clamp 到 50
	req := httptest.NewRequest("GET", "/v1/admin/audit?page=0&per_page=200", nil)
	w := httptest.NewRecorder()
	handler.AuditLog(w, req)

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["page"].(float64) != 1 {
		t.Errorf("page = %v, want clamped to 1", resp["page"])
	}
	if resp["per_page"].(float64) != 50 {
		t.Errorf("per_page = %v, want clamped to 50", resp["per_page"])
	}
}

// v0.10.0 新增：DB 错误返回 500
func TestAdminHandler_AuditLog_DBError(t *testing.T) {
	meta := &mockAdminMeta{auditErr: errors.New("database down")}
	handler := NewAdminHandler(meta, &mockAdminWP{}, "", "", "", "")

	req := httptest.NewRequest("GET", "/v1/admin/audit", nil)
	w := httptest.NewRecorder()
	handler.AuditLog(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

// v0.10.0 新增：零计数场景
func TestAdminHandler_Status_ZeroCounts(t *testing.T) {
	meta := &mockAdminMeta{fileCount: 0, blobCount: 0, totalSize: 0}
	handler := NewAdminHandler(meta, &mockAdminWP{}, "", "", "", "")

	req := httptest.NewRequest("GET", "/v1/admin/status", nil)
	w := httptest.NewRecorder()
	handler.Status(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	storage := resp["storage"].(map[string]any)
	if storage["total_files"].(float64) != 0 {
		t.Errorf("total_files = %v, want 0", storage["total_files"])
	}
	if storage["total_blobs"].(float64) != 0 {
		t.Errorf("total_blobs = %v, want 0", storage["total_blobs"])
	}
}
