package fileupload

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// newUploadMockServer 创建模拟后端的 httptest.Server
// 实现：POST /v1/uploads/init, PUT /v1/uploads/{id}/chunks/{n}, POST /v1/uploads/{id}/finalize
//
//	POST /v1/dirs, POST /v1/batch/{action}, POST /v1/admin/scan, DELETE /v1/{files,dirs}/{id}
func newUploadMockServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 移除 namespace 参数后再做 path 匹配（SDK 的 url() 自动追加 namespace）
		// 但 path 本身不变，只是 query 多了一个 namespace=
		path := r.URL.Path

		switch {
		case path == "/uploads" && r.Method == http.MethodPost:
			// tus 协议：返回 Location + Upload-Offset
			w.Header().Set("Location", "/uploads/sess-1")
			w.Header().Set("Upload-Offset", "0")
			w.WriteHeader(http.StatusCreated)

		case strings.HasPrefix(path, "/uploads/") && r.Method == http.MethodPatch:
			// tus 协议：PATCH 上传分片（不分 /chunks/ 子路径）
			w.WriteHeader(http.StatusNoContent)

		case strings.HasPrefix(path, "/uploads/") && r.Method == http.MethodHead:
			// tus 协议：HEAD 查询进度
			w.Header().Set("Upload-Offset", "100")
			w.WriteHeader(http.StatusOK)

		case strings.HasPrefix(path, "/uploads/") && r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)

		case path == "/v1/uploads/init" && r.Method == http.MethodPost:
			// 备用：REST 协议
			var req map[string]any
			json.NewDecoder(r.Body).Decode(&req)
			jsonResp(w, http.StatusOK, map[string]any{
				"session_id": "sess-1",
				"chunk_size": 1024 * 1024,
			})

		case strings.Contains(path, "/chunks/") && r.Method == http.MethodPut:
			w.WriteHeader(http.StatusOK)

		case path == "/v1/files" && r.Method == http.MethodHead:
			// 秒传预检：mock 总是返回 404（不存在）
			w.WriteHeader(http.StatusNotFound)

		case strings.Contains(path, "/finalize") && r.Method == http.MethodPost:
			jsonResp(w, http.StatusOK, map[string]any{
				"file_id": "file-1",
				"sha256":  "deadbeef",
				"size":    100,
				"name":    "test.bin",
			})

		case path == "/v1/dirs" && r.Method == http.MethodPost:
			var req map[string]any
			json.NewDecoder(r.Body).Decode(&req)
			jsonResp(w, http.StatusCreated, map[string]string{"file_id": "dir-1"})

		case strings.HasPrefix(path, "/v1/dirs/") && r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)

		case strings.HasPrefix(path, "/v1/files/") && r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)

		case path == "/v1/batch/copy" && r.Method == http.MethodPost:
			jsonResp(w, http.StatusOK, map[string]any{"success": 2, "failed": 0})

		case path == "/v1/admin/scan" && r.Method == http.MethodPost:
			jsonResp(w, http.StatusOK, map[string]any{
				"orphan_parts": 0, "orphan_files": []string{},
				"metadata_orphans": 0, "ref_count_fixes": 0, "corrupted_files": []string{},
			})

		case strings.HasPrefix(path, "/s/"):
			jsonResp(w, http.StatusOK, map[string]any{
				"token":   strings.TrimPrefix(path, "/s/"),
				"file_id": "file-1",
			})

		default:
			t.Logf("UNHANDLED: %s %s", r.Method, r.URL.String())
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

type recordedUpload struct {
	name    string
	sha256  string
	chunks  map[int][]byte
	offsets map[int]int64
}

type uploadRecorder struct {
	mu       sync.Mutex
	nextID   int
	sessions map[string]*recordedUpload
	manifest dirManifest
}

func newRecordingUploadServer(t *testing.T) (*httptest.Server, *uploadRecorder) {
	t.Helper()
	recorder := &uploadRecorder{sessions: make(map[string]*recordedUpload)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case path == "/v1/files" && r.Method == http.MethodHead:
			w.WriteHeader(http.StatusNotFound)

		case path == "/uploads" && r.Method == http.MethodPost:
			recorder.mu.Lock()
			recorder.nextID++
			sessionID := "sess-" + strconv.Itoa(recorder.nextID)
			recorder.sessions[sessionID] = &recordedUpload{
				name:    r.Header.Get("X-File-Name"),
				sha256:  r.Header.Get("X-SHA256"),
				chunks:  make(map[int][]byte),
				offsets: make(map[int]int64),
			}
			recorder.mu.Unlock()
			w.Header().Set("Location", "/uploads/"+sessionID)
			w.WriteHeader(http.StatusCreated)

		case strings.HasPrefix(path, "/uploads/") && r.Method == http.MethodPatch:
			sessionID := strings.TrimPrefix(path, "/uploads/")
			index, err := strconv.Atoi(r.Header.Get("X-Slice-Index"))
			if err != nil {
				http.Error(w, "invalid slice index", http.StatusBadRequest)
				return
			}
			offset, err := strconv.ParseInt(r.Header.Get("Upload-Offset"), 10, 64)
			if err != nil {
				http.Error(w, "invalid upload offset", http.StatusBadRequest)
				return
			}
			data, err := io.ReadAll(r.Body)
			if err != nil || SHA256Sum(data) != r.Header.Get("X-Slice-SHA256") {
				http.Error(w, "invalid chunk checksum", http.StatusBadRequest)
				return
			}
			recorder.mu.Lock()
			session := recorder.sessions[sessionID]
			if session != nil {
				session.chunks[index] = append([]byte(nil), data...)
				session.offsets[index] = offset
			}
			recorder.mu.Unlock()
			if session == nil {
				http.Error(w, "unknown session", http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusNoContent)

		case strings.HasPrefix(path, "/v1/uploads/") && strings.HasSuffix(path, "/finalize") && r.Method == http.MethodPost:
			sessionID := strings.TrimSuffix(strings.TrimPrefix(path, "/v1/uploads/"), "/finalize")
			data, name, sha256, ok := recorder.sessionSnapshot(sessionID)
			if !ok {
				http.Error(w, "unknown session", http.StatusNotFound)
				return
			}
			if SHA256Sum(data) != sha256 {
				http.Error(w, "invalid file checksum", http.StatusBadRequest)
				return
			}
			jsonResp(w, http.StatusOK, FileInfo{
				FileID: "file-" + sessionID,
				SHA256: sha256,
				Size:   int64(len(data)),
				Name:   name,
			})

		case path == "/v1/dirs" && r.Method == http.MethodPost:
			var manifest dirManifest
			if err := json.NewDecoder(r.Body).Decode(&manifest); err != nil {
				http.Error(w, "invalid manifest", http.StatusBadRequest)
				return
			}
			recorder.mu.Lock()
			recorder.manifest = dirManifest{Name: manifest.Name, Entries: append([]DirEntry(nil), manifest.Entries...)}
			recorder.mu.Unlock()
			jsonResp(w, http.StatusCreated, FileInfo{FileID: "dir-1", Name: manifest.Name})

		default:
			t.Logf("UNHANDLED recording server request: %s %s", r.Method, r.URL.String())
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	return server, recorder
}

func (r *uploadRecorder) sessionSnapshot(sessionID string) ([]byte, string, string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	session := r.sessions[sessionID]
	if session == nil {
		return nil, "", "", false
	}
	indexes := make([]int, 0, len(session.chunks))
	for index := range session.chunks {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	var data []byte
	var expectedOffset int64
	for _, index := range indexes {
		if session.offsets[index] != expectedOffset {
			return nil, session.name, session.sha256, false
		}
		data = append(data, session.chunks[index]...)
		expectedOffset += int64(len(session.chunks[index]))
	}
	return data, session.name, session.sha256, true
}

func (r *uploadRecorder) sessionByName(name string) ([]byte, string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for sessionID, session := range r.sessions {
		if session.name != name {
			continue
		}
		indexes := make([]int, 0, len(session.chunks))
		for index := range session.chunks {
			indexes = append(indexes, index)
		}
		sort.Ints(indexes)
		var data []byte
		for _, index := range indexes {
			data = append(data, session.chunks[index]...)
		}
		return data, "file-" + sessionID, true
	}
	return nil, "", false
}

func (r *uploadRecorder) manifestSnapshot() dirManifest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return dirManifest{Name: r.manifest.Name, Entries: append([]DirEntry(nil), r.manifest.Entries...)}
}

// TestUploadReader_FullFlow 端到端：CreateSession + UploadChunk + Finalize
func TestUploadReader_FullFlow(t *testing.T) {
	srv := newUploadMockServer(t)
	defer srv.Close()

	c := NewClient(srv.URL, "test")
	data := []byte("hello world, this is a test file content")

	info, err := c.UploadReader(context.Background(), bytes.NewReader(data), int64(len(data)), "test.txt")
	if err != nil {
		t.Fatalf("UploadReader error = %v", err)
	}
	if info.FileID != "file-1" {
		t.Errorf("FileID = %s, want file-1", info.FileID)
	}
	if info.Name != "test.bin" {
		t.Errorf("Name = %s, want test.bin", info.Name)
	}
}

func TestUploadReader_UsesEffectiveFileNameAndSize(t *testing.T) {
	server, recorder := newRecordingUploadServer(t)
	defer server.Close()

	data := bytes.Repeat([]byte("reader-data-"), 17)
	client := NewClient(server.URL, "test")
	info, err := client.UploadReader(
		context.Background(),
		bytes.NewReader(data),
		int64(len(data)),
		"original.bin",
		WithFileName("renamed.bin"),
		WithChunkSize(11),
		WithConcurrency(2),
	)
	if err != nil {
		t.Fatalf("UploadReader error = %v", err)
	}
	if info.Name != "renamed.bin" {
		t.Fatalf("Name = %q, want renamed.bin", info.Name)
	}
	uploaded, _, ok := recorder.sessionByName("renamed.bin")
	if !ok || !bytes.Equal(uploaded, data) {
		t.Fatalf("recorded reader upload does not match source data")
	}
}

// TestUpload_FromTempFile 覆盖真实文件读取、多分片并发上传和完整性校验。
func TestUpload_FromTempFile(t *testing.T) {
	server, recorder := newRecordingUploadServer(t)
	defer server.Close()

	data := bytes.Repeat([]byte("concurrent-upload-check-"), 31)
	path := filepath.Join(t.TempDir(), "payload.bin")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := NewClient(server.URL, "test")
	info, err := client.Upload(ctx, path, WithChunkSize(32), WithConcurrency(2))
	if err != nil {
		t.Fatalf("Upload error = %v", err)
	}
	if info.Name != "payload.bin" {
		t.Fatalf("Name = %q, want payload.bin", info.Name)
	}
	if info.SHA256 != SHA256Sum(data) {
		t.Fatalf("SHA256 = %q, want %q", info.SHA256, SHA256Sum(data))
	}

	uploaded, fileID, ok := recorder.sessionByName("payload.bin")
	if !ok {
		t.Fatal("recorded upload not found")
	}
	if info.FileID != fileID {
		t.Fatalf("FileID = %q, want %q", info.FileID, fileID)
	}
	if !bytes.Equal(uploaded, data) {
		t.Fatalf("uploaded data mismatch: got %d bytes, want %d", len(uploaded), len(data))
	}
}

func TestFileSHA256(t *testing.T) {
	data := []byte("file hashing must use the local filesystem")
	path := filepath.Join(t.TempDir(), "hash.txt")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	got, err := FileSHA256(path)
	if err != nil {
		t.Fatalf("FileSHA256 error = %v", err)
	}
	if want := SHA256Sum(data); got != want {
		t.Fatalf("FileSHA256 = %q, want %q", got, want)
	}
}

func TestUpload_InstantUploadParsesHeadMetadata(t *testing.T) {
	data := []byte("already stored content")
	path := filepath.Join(t.TempDir(), "existing.txt")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead || r.URL.Path != "/v1/files" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if got := r.URL.Query().Get("sha256"); got != SHA256Sum(data) {
			t.Errorf("sha256 query = %q, want %q", got, SHA256Sum(data))
		}
		w.Header().Set("X-File-ID", "instant-file")
		w.Header().Set("X-File-SHA256", SHA256Sum(data))
		w.Header().Set("X-File-Size", strconv.Itoa(len(data)))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test")
	info, err := client.Upload(context.Background(), path)
	if err != nil {
		t.Fatalf("Upload error = %v", err)
	}
	if info.FileID != "instant-file" || info.Name != "existing.txt" || info.Size != int64(len(data)) {
		t.Fatalf("instant upload info = %+v", info)
	}
}

func TestUploadReader_RejectsInvalidOptions(t *testing.T) {
	tests := []struct {
		name string
		opt  UploadOption
		want string
	}{
		{name: "zero chunk size", opt: WithChunkSize(0), want: "分片大小必须大于 0"},
		{name: "negative chunk size", opt: WithChunkSize(-1), want: "分片大小必须大于 0"},
		{name: "zero concurrency", opt: WithConcurrency(0), want: "上传并发数必须大于 0"},
		{name: "negative concurrency", opt: WithConcurrency(-1), want: "上传并发数必须大于 0"},
		{name: "unsupported compression", opt: WithCompression("gzip"), want: "不支持的压缩格式"},
	}

	client := NewClient("http://127.0.0.1:1", "test")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.UploadReader(context.Background(), strings.NewReader("data"), 4, "data.txt", tt.opt)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("UploadReader error = %v, want containing %q", err, tt.want)
			}
		})
	}

	_, err := client.UploadReader(context.Background(), strings.NewReader("data"), 5, "data.txt")
	if err == nil || !strings.Contains(err.Error(), "文件大小不匹配") {
		t.Fatalf("UploadReader size mismatch error = %v", err)
	}
}

// TestCreateSession 单独测试 session 创建
func TestCreateSession(t *testing.T) {
	srv := newUploadMockServer(t)
	defer srv.Close()

	c := NewClient(srv.URL, "test")
	sessID, err := c.CreateSession(context.Background(), 100, "abc123", "none", 1024, "f.txt")
	if err != nil {
		t.Fatalf("CreateSession error = %v", err)
	}
	if sessID != "sess-1" {
		t.Errorf("sessionID = %s, want sess-1", sessID)
	}
}

// TestUploadChunk 单独测试分片上传
func TestUploadChunk(t *testing.T) {
	srv := newUploadMockServer(t)
	defer srv.Close()

	c := NewClient(srv.URL, "test")
	data := []byte("chunk data")
	if err := c.UploadChunk(context.Background(), "sess-1", 0, data, 0); err != nil {
		t.Fatalf("UploadChunk error = %v", err)
	}
}

// TestFinalize 单独测试 finalize
func TestFinalize(t *testing.T) {
	srv := newUploadMockServer(t)
	defer srv.Close()

	c := NewClient(srv.URL, "test")
	info, err := c.Finalize(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("Finalize error = %v", err)
	}
	if info == nil {
		t.Fatal("info is nil")
	}
	if info.SHA256 != "deadbeef" {
		t.Errorf("SHA256 = %s", info.SHA256)
	}
}

// TestUploadDir 覆盖目录遍历、并发文件上传和稳定 manifest 顺序。
func TestUploadDir(t *testing.T) {
	server, recorder := newRecordingUploadServer(t)
	defer server.Close()

	dirPath := filepath.Join(t.TempDir(), "documents")
	if err := os.MkdirAll(filepath.Join(dirPath, "nested"), 0o755); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	files := map[string][]byte{
		"documents/a.txt":        []byte("alpha-content"),
		"documents/nested/b.txt": bytes.Repeat([]byte("beta"), 19),
	}
	for name, data := range files {
		path := filepath.Join(filepath.Dir(dirPath), filepath.FromSlash(name))
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", name, err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := NewClient(server.URL, "test")
	info, err := client.UploadDir(ctx, dirPath, WithChunkSize(7), WithConcurrency(2))
	if err != nil {
		t.Fatalf("UploadDir error = %v", err)
	}
	if info.FileID != "dir-1" {
		t.Fatalf("FileID = %q, want dir-1", info.FileID)
	}

	manifest := recorder.manifestSnapshot()
	if manifest.Name != "documents" {
		t.Fatalf("manifest name = %q, want documents", manifest.Name)
	}
	if len(manifest.Entries) != 2 {
		t.Fatalf("manifest entries = %d, want 2", len(manifest.Entries))
	}
	wantPaths := []string{"documents/a.txt", "documents/nested/b.txt"}
	for i, wantPath := range wantPaths {
		entry := manifest.Entries[i]
		if entry.Path != wantPath {
			t.Fatalf("entry[%d].Path = %q, want %q", i, entry.Path, wantPath)
		}
		uploaded, fileID, ok := recorder.sessionByName(wantPath)
		if !ok {
			t.Fatalf("upload %q not recorded", wantPath)
		}
		if entry.FileID != fileID {
			t.Fatalf("entry[%d].FileID = %q, want %q", i, entry.FileID, fileID)
		}
		if !bytes.Equal(uploaded, files[wantPath]) {
			t.Fatalf("uploaded data for %q does not match", wantPath)
		}
	}
}

func TestUploadDir_PropagatesWalkErrors(t *testing.T) {
	missingPath := filepath.Join(t.TempDir(), "missing")
	client := NewClient("http://127.0.0.1:1", "test")
	_, err := client.UploadDir(context.Background(), missingPath)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("UploadDir error = %v, want fs.ErrNotExist", err)
	}
}

// TestSubmitDir 提交目录 manifest
func TestSubmitDir(t *testing.T) {
	srv := newUploadMockServer(t)
	defer srv.Close()

	c := NewClient(srv.URL, "test")
	info, err := c.SubmitDir(context.Background(), "mydir", []DirEntry{
		{Path: "a.txt", FileID: "f1"},
		{Path: "b.txt", FileID: "f2"},
	})
	if err != nil {
		t.Fatalf("SubmitDir error = %v", err)
	}
	if info == nil {
		t.Fatal("info is nil")
	}
	if info.FileID != "dir-1" {
		t.Errorf("FileID = %s, want dir-1", info.FileID)
	}
}

// TestBatchCopy 批量复制
func TestBatchCopy(t *testing.T) {
	srv := newUploadMockServer(t)
	defer srv.Close()

	c := NewClient(srv.URL, "test")
	res, err := c.BatchCopy(context.Background(), []string{"f1", "f2"}, "target")
	if err != nil {
		t.Fatalf("BatchCopy error = %v", err)
	}
	if res.Success != 2 {
		t.Errorf("Success = %d, want 2", res.Success)
	}
}

// TestUploadReader_ContextCancel 验证 ctx 取消
func TestUploadReader_ContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second) // 故意慢
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test")
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	_, err := c.UploadReader(ctx, strings.NewReader("x"), 1, "x.txt")
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

// TestUploadReader_LargeFile 大文件分片
func TestUploadReader_LargeFile(t *testing.T) {
	srv := newUploadMockServer(t)
	defer srv.Close()

	c := NewClient(srv.URL, "test")
	// 3 MB 数据，需要 2 个分片（chunk_size 默认 1MB，但 mock server 不验证大小）
	data := bytes.Repeat([]byte("X"), 3*1024*1024)
	info, err := c.UploadReader(context.Background(), bytes.NewReader(data), int64(len(data)), "big.bin")
	if err != nil {
		t.Fatalf("UploadReader error = %v", err)
	}
	if info == nil {
		t.Fatal("info is nil")
	}
}

// TestDeleteDir 删除目录
func TestDeleteDir(t *testing.T) {
	srv := newUploadMockServer(t)
	defer srv.Close()

	c := NewClient(srv.URL, "test")
	if err := c.DeleteDir(context.Background(), "dir-1"); err != nil {
		t.Errorf("DeleteDir error = %v", err)
	}
}

// TestPreview_BlobResponse 预览返回 Response
func TestPreview_BlobResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("PNG-DATA"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test")
	resp, err := c.Preview(context.Background(), "file-1")
	if err != nil {
		t.Fatalf("Preview error = %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "PNG-DATA" {
		t.Errorf("body = %s", body)
	}
}
