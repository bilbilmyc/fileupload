package fileupload

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestSubmitDir_ExistingAPI 验证已有 SubmitDir(ctx, name, entries) 工作
func TestSubmitDir_ExistingAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/dirs" {
			t.Errorf("path = %s, want /v1/dirs", r.URL.Path)
		}
		var manifest map[string]any
		json.NewDecoder(r.Body).Decode(&manifest)
		entries, _ := manifest["entries"].([]any)
		if len(entries) != 2 {
			t.Errorf("entries = %d, want 2", len(entries))
		}
		jsonResp(w, http.StatusCreated, map[string]any{
			"file_id": "dir-xyz",
			"name":    "mydir",
			"sha256":  "",
			"size":    0,
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "default")
	info, err := c.SubmitDir(context.Background(), "mydir", []clientDirEntry{
		{Path: "a.txt", FileID: "f1"},
		{Path: "b.txt", FileID: "f2"},
	})
	if err != nil {
		t.Fatalf("SubmitDir error = %v", err)
	}
	if info.FileID != "dir-xyz" {
		t.Errorf("FileID = %s, want dir-xyz", info.FileID)
	}
}

func TestGetUploadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/uploads/sess-1/status" {
			t.Errorf("path = %s", r.URL.Path)
		}
		jsonResp(w, http.StatusOK, map[string]any{
			"session_id": "sess-1",
			"chunks": []map[string]any{
				{"index": 0, "sha256": "sha-a", "size": 1024},
				{"index": 1, "sha256": "sha-b", "size": 2048},
			},
			"total": 2,
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "default")
	st, err := c.GetUploadStatus(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("GetUploadStatus error = %v", err)
	}
	if st.SessionID != "sess-1" {
		t.Errorf("SessionID = %s", st.SessionID)
	}
	if st.Total != 2 {
		t.Errorf("Total = %d, want 2", st.Total)
	}
	if len(st.Chunks) != 2 {
		t.Errorf("Chunks len = %d", len(st.Chunks))
	}
}

func TestCancelUpload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		if r.URL.Path != "/uploads/sess-1" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "default")
	if err := c.CancelUpload(context.Background(), "sess-1"); err != nil {
		t.Fatalf("CancelUpload error = %v", err)
	}
}

func TestPreviewURL(t *testing.T) {
	c := NewClient("http://example.com", "ns1")
	got := c.PreviewURL("file-abc")
	want := "http://example.com/v1/preview/file-abc?namespace=ns1"
	if got != want {
		t.Errorf("PreviewURL = %q, want %q", got, want)
	}
}