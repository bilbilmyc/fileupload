package fileupload

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRename(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("method = %s, want PATCH", r.Method)
		}
		if r.URL.Path != "/v1/files/file-1" {
			t.Errorf("path = %s", r.URL.Path)
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "newname.txt" {
			t.Errorf("name = %q", body["name"])
		}
		jsonResp(w, http.StatusOK, map[string]string{"status": "ok"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "default")
	if err := c.Rename(context.Background(), "file-1", "newname.txt"); err != nil {
		t.Fatalf("Rename error = %v", err)
	}
}

func TestCreateShare(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/share" {
			t.Errorf("path = %s", r.URL.Path)
		}
		var req CreateShareRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.FileID != "file-1" || req.ExpiresIn != 24 {
			t.Errorf("unexpected req: %+v", req)
		}
		jsonResp(w, http.StatusCreated, map[string]any{
			"token":         "s-abc123",
			"file_id":       req.FileID,
			"expires_at":    "2026-12-31T00:00:00Z",
			"max_downloads": 10,
			"cur_downloads": 0,
			"namespace":     "default",
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "default")
	entry, err := c.CreateShare(context.Background(), CreateShareRequest{
		FileID: "file-1", ExpiresIn: 24, MaxDownloads: 10,
	})
	if err != nil {
		t.Fatalf("CreateShare error = %v", err)
	}
	if entry.Token != "s-abc123" {
		t.Errorf("Token = %s, want s-abc123", entry.Token)
	}
}

func TestAccessShare(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/s/s-abc123" {
			t.Errorf("path = %s", r.URL.Path)
		}
		jsonResp(w, http.StatusOK, map[string]any{
			"token":         "s-abc123",
			"file_id":       "file-1",
			"cur_downloads": 3,
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "default")
	entry, err := c.AccessShare(context.Background(), "s-abc123")
	if err != nil {
		t.Fatalf("AccessShare error = %v", err)
	}
	if entry.FileID != "file-1" {
		t.Errorf("FileID = %s", entry.FileID)
	}
}

func TestShareURL(t *testing.T) {
	c := NewClient("http://example.com", "ns")
	got := c.ShareURL("s-xyz")
	want := "http://example.com/s/s-xyz?namespace=ns"
	if got != want {
		t.Errorf("ShareURL = %q, want %q", got, want)
	}
}

func TestGetSystemStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/admin/status" {
			t.Errorf("path = %s", r.URL.Path)
		}
		jsonResp(w, http.StatusOK, map[string]any{
			"version":    "v0.5.0",
			"uptime":     "24h",
			"start_time": "2026-06-23T00:00:00Z",
			"counts":     map[string]int{"files": 100, "blobs": 50},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "default")
	status, err := c.GetSystemStatus(context.Background())
	if err != nil {
		t.Fatalf("GetSystemStatus error = %v", err)
	}
	if status.Version != "v0.5.0" {
		t.Errorf("Version = %s", status.Version)
	}
	if status.Counts["files"] != 100 {
		t.Errorf("counts[files] = %d, want 100", status.Counts["files"])
	}
}

func TestListAuditLogs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/admin/audit" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.URL.Query().Get("page") != "1" || r.URL.Query().Get("per_page") != "20" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		jsonResp(w, http.StatusOK, map[string]any{
			"entries": []map[string]any{
				{"id": "log-1", "target_type": "file", "target_id": "f1", "namespace": "default", "detail": "delete"},
			},
			"total": 1,
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "default")
	entries, total, err := c.ListAuditLogs(context.Background(), 1, 20)
	if err != nil {
		t.Fatalf("ListAuditLogs error = %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if len(entries) != 1 || entries[0].ID != "log-1" {
		t.Errorf("entries = %+v", entries)
	}
}

func TestTriggerScan(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/admin/scan" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		jsonResp(w, http.StatusOK, map[string]any{
			"orphan_parts":     0,
			"orphan_files":     []string{},
			"metadata_orphans": 0,
			"ref_count_fixes":  0,
			"corrupted_files":  []string{},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "default")
	report, err := c.TriggerScan(context.Background())
	if err != nil {
		t.Fatalf("TriggerScan error = %v", err)
	}
	if report == nil {
		t.Fatal("report is nil")
	}
}