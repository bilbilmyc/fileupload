package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

// ===== Test helpers =====

func newTestFixtures(t *testing.T) (*domain.UploadService, *domain.DownloadService, *TusHandler, *RESTHandler, *DownloadHandler) {
	t.Helper()
	meta := newMockMeta()
	storage := newMockStore()
	compress := newMockCompr()
	hasher := newMockHashr()
	pool := newMockWP()

	ucfg := domain.UploadConfig{
		SessionTTL:       time.Hour,
		DataDir:          "data",
		DefaultChunkSize: 1024 * 1024,
	}
	uploadSvc := domain.NewUploadService(meta, storage, storage, compress, hasher, pool, ucfg)

	dcfg := domain.DownloadConfig{DataDir: "data"}
	downloadSvc := domain.NewDownloadService(meta, storage, compress, hasher, dcfg)

	tusHandler := NewTusHandler(uploadSvc)
	restHandler := NewRESTHandler(uploadSvc, downloadSvc)
	downloadHandler := NewDownloadHandler(downloadSvc)

	return uploadSvc, downloadSvc, tusHandler, restHandler, downloadHandler
}

func withNamespace(r *http.Request, ns string) *http.Request {
	ctx := context.WithValue(r.Context(), ctxKeyNamespace, ns)
	return r.WithContext(ctx)
}

// ===== tus handler tests =====

func TestCreateUpload_Success(t *testing.T) {
	_, _, tusHandler, _, _ := newTestFixtures(t)

	req := httptest.NewRequest("POST", "/uploads", nil)
	req.Header.Set("Upload-Length", "1000")
	req.Header.Set("X-SHA256", "abc123")
	req.Header.Set("X-Compression", "zstd")
	req.Header.Set("X-Chunk-Size", "256")
	req.Header.Set("X-File-Name", "test.bin")
	req = withNamespace(req, "demo")

	w := httptest.NewRecorder()
	tusHandler.CreateUpload(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", w.Code)
	}
	if w.Header().Get("Location") == "" {
		t.Error("Location header is empty")
	}
	if w.Header().Get("Upload-Offset") != "0" {
		t.Errorf("Upload-Offset = %s", w.Header().Get("Upload-Offset"))
	}
}

func TestCreateUpload_MissingLength(t *testing.T) {
	_, _, tusHandler, _, _ := newTestFixtures(t)
	w := httptest.NewRecorder()
	tusHandler.CreateUpload(w, httptest.NewRequest("POST", "/uploads", nil))
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestCreateUpload_ZeroLength(t *testing.T) {
	_, _, tusHandler, _, _ := newTestFixtures(t)
	req := httptest.NewRequest("POST", "/uploads", nil)
	req.Header.Set("Upload-Length", "0")
	w := httptest.NewRecorder()
	tusHandler.CreateUpload(w, req)
	if w.Code != http.StatusCreated {
		t.Errorf("0-length upload status = %d, want 201", w.Code)
	}
}

func TestCreateUpload_InvalidLength(t *testing.T) {
	_, _, tusHandler, _, _ := newTestFixtures(t)
	req := httptest.NewRequest("POST", "/uploads", nil)
	req.Header.Set("Upload-Length", "abc")
	w := httptest.NewRecorder()
	tusHandler.CreateUpload(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestGetUploadInfo_NotFound(t *testing.T) {
	_, _, tusHandler, _, _ := newTestFixtures(t)
	req := httptest.NewRequest("HEAD", "/uploads/no-such", nil)
	w := httptest.NewRecorder()
	tusHandler.GetUploadInfo(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestGetUploadInfo_Success(t *testing.T) {
	uploadSvc, _, tusHandler, _, _ := newTestFixtures(t)
	ctx := context.Background()

	session, _ := uploadSvc.CreateSession(ctx, "sha", 1000, domain.CompNone, 100, "demo", "f.bin")

	req := httptest.NewRequest("HEAD", "/uploads/"+session.SessionID, nil)
	w := httptest.NewRecorder()
	tusHandler.GetUploadInfo(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if w.Header().Get("Upload-Offset") == "" {
		t.Error("Upload-Offset header is empty")
	}
}

func TestAppendChunk_InvalidSession(t *testing.T) {
	_, _, tusHandler, _, _ := newTestFixtures(t)
	req := httptest.NewRequest("PATCH", "/uploads/no-such", strings.NewReader("data"))
	w := httptest.NewRecorder()
	tusHandler.AppendChunk(w, req)
	// no-such session should error
	if w.Code == http.StatusOK || w.Code == http.StatusNoContent {
		t.Errorf("unexpected success status = %d", w.Code)
	}
}

// ===== REST handler tests =====
func TestCancelUpload_Success(t *testing.T) {
	uploadSvc, _, tusHandler, _, _ := newTestFixtures(t)
	ctx := context.Background()
	session, _ := uploadSvc.CreateSession(ctx, "sha", 100, domain.CompNone, 10, "demo", "f.txt")

	req := httptest.NewRequest("DELETE", "/uploads/"+session.SessionID, nil)
	w := httptest.NewRecorder()
	tusHandler.CancelUpload(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}
}

func TestCancelUpload_NotFound(t *testing.T) {
	_, _, tusHandler, _, _ := newTestFixtures(t)
	req := httptest.NewRequest("DELETE", "/uploads/no-such", nil)
	w := httptest.NewRecorder()
	tusHandler.CancelUpload(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestREST_InitUpload(t *testing.T) {
	_, _, _, restHandler, _ := newTestFixtures(t)
	req := httptest.NewRequest("POST", "/v1/uploads/init?size=500", nil)
	req.Header.Set("X-SHA256", "sha-test")
	req = withNamespace(req, "demo")
	w := httptest.NewRecorder()
	restHandler.InitUpload(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["session_id"] == "" {
		t.Error("session_id is empty")
	}
}

func TestREST_InitUpload_NoSize(t *testing.T) {
	_, _, _, restHandler, _ := newTestFixtures(t)
	req := httptest.NewRequest("POST", "/v1/uploads/init", nil)
	w := httptest.NewRecorder()
	restHandler.InitUpload(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestREST_UploadChunk(t *testing.T) {
	uploadSvc, _, _, restHandler, _ := newTestFixtures(t)
	ctx := context.Background()
	session, _ := uploadSvc.CreateSession(ctx, "sha", 100, domain.CompNone, 100, "demo", "f.bin")

	req := httptest.NewRequest("PUT", fmt.Sprintf("/v1/uploads/%s/chunks/0", session.SessionID), strings.NewReader("chunk data"))
	w := httptest.NewRecorder()
	restHandler.UploadChunk(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestREST_GetUploadStatus(t *testing.T) {
	uploadSvc, _, _, restHandler, _ := newTestFixtures(t)
	ctx := context.Background()
	session, _ := uploadSvc.CreateSession(ctx, "sha", 100, domain.CompNone, 10, "demo", "f.txt")

	req := httptest.NewRequest("GET", "/v1/uploads/"+session.SessionID+"/status", nil)
	req = withNamespace(req, "demo")
	w := httptest.NewRecorder()
	restHandler.GetUploadStatus(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestREST_FinalizeUpload(t *testing.T) {
	uploadSvc, _, _, restHandler, _ := newTestFixtures(t)
	ctx := context.Background()

	content := []byte("finalize test content for upload")
	session, _ := uploadSvc.CreateSession(ctx, "", int64(len(content)), domain.CompNone, 100, "demo", "f.txt")
	uploadSvc.AppendChunk(ctx, session.SessionID, 0, bytes.NewReader(content), "")

	req := httptest.NewRequest("POST", fmt.Sprintf("/v1/uploads/%s/finalize", session.SessionID), nil)
	req = withNamespace(req, "demo")
	w := httptest.NewRecorder()
	restHandler.FinalizeUpload(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["file_id"] == "" {
		t.Error("file_id is empty")
	}
}

// ===== Download handler tests =====

func TestDownloadHandler_GetFile(t *testing.T) {
	uploadSvc, downloadSvc, _, _, downloadHandler := newTestFixtures(t)
	ctx := context.Background()

	content := []byte("download test data")
	session, _ := uploadSvc.CreateSession(ctx, "", int64(len(content)), domain.CompNone, 100, "demo", "d.txt")
	uploadSvc.AppendChunk(ctx, session.SessionID, 0, bytes.NewReader(content), "")
	result, _ := uploadSvc.Finalize(ctx, session.SessionID)

	req := httptest.NewRequest("GET", "/v1/files/"+result.FileID, nil)
	req = withNamespace(req, "demo")
	w := httptest.NewRecorder()
	downloadHandler.GetFile(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if w.Header().Get("X-SHA256") == "" {
		t.Error("X-SHA256 header is empty")
	}
	body, _ := io.ReadAll(w.Body)
	if !bytes.Equal(body, content) {
		t.Errorf("body mismatch: got %s, want %s", body, content)
	}

	_ = downloadSvc // 防止 unused 警告
}

func TestDownloadHandler_GetFile_NotFound(t *testing.T) {
	_, _, _, _, downloadHandler := newTestFixtures(t)
	req := httptest.NewRequest("GET", "/v1/files/no-such", nil)
	req = withNamespace(req, "demo")
	w := httptest.NewRecorder()
	downloadHandler.GetFile(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestDownloadHandler_GetDir(t *testing.T) {
	uploadSvc, downloadSvc, _, _, downloadHandler := newTestFixtures(t)
	ctx := context.Background()

	// 先上传一个文件
	content := []byte("dir content")
	session, _ := uploadSvc.CreateSession(ctx, "", int64(len(content)), domain.CompNone, 100, "demo", "d2.txt")
	uploadSvc.AppendChunk(ctx, session.SessionID, 0, bytes.NewReader(content), "")
	result, _ := uploadSvc.Finalize(ctx, session.SessionID)

	// 创建目录
	manifest := domain.DirManifest{
		Entries: []domain.DirEntry{{Path: "f1.txt", FileID: result.FileID}},
	}
	dir, _ := uploadSvc.SubmitDir(ctx, manifest, "demo")

	req := httptest.NewRequest("GET", "/v1/dirs/"+dir.FileID+"?format=tar.gz", nil)
	req = withNamespace(req, "demo")
	w := httptest.NewRecorder()
	downloadHandler.GetDir(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if w.Header().Get("X-Tree-SHA256") == "" {
		t.Error("X-Tree-SHA256 header is empty")
	}
	body, _ := io.ReadAll(w.Body)
	if len(body) == 0 {
		t.Error("dir download body is empty")
	}

	_ = downloadSvc
}

// ===== Router tests =====

func TestRouter_Health(t *testing.T) {
	_, _, tusHandler, restHandler, downloadHandler := newTestFixtures(t)
	mw := NewMiddleware()
	uploadSvc, _, _, _, _ := newTestFixtures(t)
	_ = uploadSvc
	router := NewRouter(mw, tusHandler, restHandler, downloadHandler, nil, nil, nil, nil, nil, nil, nil, nil)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	router.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("health status = %d, want 200", w.Code)
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "ok" {
		t.Errorf("health status = %s", resp["status"])
	}
}

func TestRouter_CheckExists(t *testing.T) {
	uploadSvc, _, tusHandler, restHandler, downloadHandler := newTestFixtures(t)
	mw := NewMiddleware()

	// 先创建一个可秒传的文件
	ctx := context.Background()
	content := []byte("dedup content for sec transmit")
	session, _ := uploadSvc.CreateSession(ctx, "", int64(len(content)), domain.CompNone, 100, "demo", "dedup.txt")
	uploadSvc.AppendChunk(ctx, session.SessionID, 0, bytes.NewReader(content), "")
	result, _ := uploadSvc.Finalize(ctx, session.SessionID)

	router := NewRouter(mw, tusHandler, restHandler, downloadHandler, nil, nil, nil, nil, nil, uploadSvc, nil, nil)

	req := httptest.NewRequest("HEAD", "/v1/files?sha256="+result.SHA256+"&namespace=demo", nil)
	w := httptest.NewRecorder()
	router.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestRouter_CheckExists_Miss(t *testing.T) {
	uploadSvc, _, tusHandler, restHandler, downloadHandler := newTestFixtures(t)
	mw := NewMiddleware()
	router := NewRouter(mw, tusHandler, restHandler, downloadHandler, nil, nil, nil, nil, nil, uploadSvc, nil, nil)

	req := httptest.NewRequest("HEAD", "/v1/files?sha256=nonexistent&namespace=demo", nil)
	w := httptest.NewRecorder()
	router.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// ===== extractPathID test =====

func TestExtractPathID(t *testing.T) {
	tests := []struct {
		path   string
		prefix string
		want   string
	}{
		{"/uploads/abc123", "/uploads/", "abc123"},
		{"/uploads/abc123/", "/uploads/", "abc123"},
		{"/v1/files/f-id", "/v1/files/", "f-id"},
		{"/uploads/", "/uploads/", ""},
		{"/short", "/longer/", ""},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := extractPathID(tt.path, tt.prefix)
			if got != tt.want {
				t.Errorf("extractPathID = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestREST_SubmitDir(t *testing.T) {
	uploadSvc, _, _, restHandler, _ := newTestFixtures(t)
	ctx := context.Background()

	content := []byte("dir entry content")
	s, _ := uploadSvc.CreateSession(ctx, "", int64(len(content)), domain.CompNone, 100, "demo", "ef.txt")
	uploadSvc.AppendChunk(ctx, s.SessionID, 0, bytes.NewReader(content), "")
	result, _ := uploadSvc.Finalize(ctx, s.SessionID)

	manifest := map[string]any{
		"entries": []map[string]string{{"path": "sub/ef.txt", "file_id": result.FileID}},
	}
	body, _ := json.Marshal(manifest)
	req := httptest.NewRequest("POST", "/v1/dirs", bytes.NewReader(body))
	req = withNamespace(req, "demo")
	w := httptest.NewRecorder()
	restHandler.SubmitDir(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201, body: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["file_id"] == "" {
		t.Error("file_id is empty")
	}
}

func TestREST_SubmitDir_InvalidJSON(t *testing.T) {
	_, _, _, restHandler, _ := newTestFixtures(t)
	req := httptest.NewRequest("POST", "/v1/dirs", strings.NewReader("not json"))
	req = withNamespace(req, "demo")
	w := httptest.NewRecorder()
	restHandler.SubmitDir(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestREST_ListDir_Root(t *testing.T) {
	uploadSvc, _, _, restHandler, _ := newTestFixtures(t)
	ctx := context.Background()

	content := []byte("list test")
	s, _ := uploadSvc.CreateSession(ctx, "", int64(len(content)), domain.CompNone, 100, "demo", "ls.txt")
	uploadSvc.AppendChunk(ctx, s.SessionID, 0, bytes.NewReader(content), "")
	uploadSvc.Finalize(ctx, s.SessionID)

	req := httptest.NewRequest("GET", "/v1/ls?parent=/", nil)
	req = withNamespace(req, "demo")
	w := httptest.NewRecorder()
	restHandler.ListDir(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	children := resp["children"].([]any)
	if len(children) < 1 {
		t.Error("list should have children")
	}
}

func TestREST_StatFile(t *testing.T) {
	uploadSvc, _, _, restHandler, _ := newTestFixtures(t)
	ctx := context.Background()

	content := []byte("stat me")
	s, _ := uploadSvc.CreateSession(ctx, "", int64(len(content)), domain.CompNone, 100, "demo", "stat.txt")
	uploadSvc.AppendChunk(ctx, s.SessionID, 0, bytes.NewReader(content), "")
	result, _ := uploadSvc.Finalize(ctx, s.SessionID)

	req := httptest.NewRequest("GET", "/v1/stat/"+result.FileID, nil)
	req = withNamespace(req, "demo")
	w := httptest.NewRecorder()
	restHandler.StatFile(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	file := resp["file"].(map[string]any)
	if file["name"] != "stat.txt" {
		t.Errorf("name = %s", file["name"])
	}
}

func TestREST_DeleteFile(t *testing.T) {
	uploadSvc, _, _, restHandler, _ := newTestFixtures(t)
	ctx := context.Background()

	content := []byte("delete me")
	s, _ := uploadSvc.CreateSession(ctx, "", int64(len(content)), domain.CompNone, 100, "demo", "del.txt")
	uploadSvc.AppendChunk(ctx, s.SessionID, 0, bytes.NewReader(content), "")
	result, _ := uploadSvc.Finalize(ctx, s.SessionID)

	req := httptest.NewRequest("DELETE", "/v1/files/"+result.FileID, nil)
	req = withNamespace(req, "demo")
	w := httptest.NewRecorder()
	restHandler.DeleteFile(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}
}

func TestREST_UploadChunk_InvalidPath(t *testing.T) {
	_, _, _, restHandler, _ := newTestFixtures(t)
	req := httptest.NewRequest("PUT", "/v1/uploads/short/1", strings.NewReader("x"))
	w := httptest.NewRecorder()
	restHandler.UploadChunk(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestGetUploadInfo_EmptySessionID(t *testing.T) {
	_, _, tusHandler, _, _ := newTestFixtures(t)
	req := httptest.NewRequest("HEAD", "/uploads/", nil)
	w := httptest.NewRecorder()
	tusHandler.GetUploadInfo(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

