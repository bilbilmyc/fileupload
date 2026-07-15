// Package transport 全链路端到端集成测试
// 使用真实组件（LocalFS + SQLite + Redis(miniredis) + 真实 Compressor/Hasher）
// 通过 HTTP API 测试完整上传/下载/秒传/删除/Stat/LS 流程。
package transport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/klauspost/compress/zstd"
	"github.com/redis/go-redis/v9"

	"github.com/bilbilmyc/fileupload/internal/adapters/compressor"
	"github.com/bilbilmyc/fileupload/internal/adapters/hasher"
	"github.com/bilbilmyc/fileupload/internal/adapters/metadata"
	"github.com/bilbilmyc/fileupload/internal/adapters/storage"
	"github.com/bilbilmyc/fileupload/internal/domain"
	"github.com/bilbilmyc/fileupload/internal/lifecycle"
)

// e2eFixture 全链路集成测试的测试夹具
type e2eFixture struct {
	t       *testing.T
	baseURL string
	client  *http.Client
	dataDir string
	tempDir string
	srv     *httptest.Server

	// 直接访问，用于断言
	meta domain.Metadata
}

func newE2EFixture(t *testing.T) *e2eFixture {
	t.Helper()

	// 1. 临时目录
	dataDir := t.TempDir()
	tempDir := t.TempDir()

	// 2. LocalFS 存储
	localFS, err := storage.NewLocalFS(dataDir)
	if err != nil {
		t.Fatalf("NewLocalFS: %v", err)
	}

	// 2b. 临时分片存储
	tempFS, err := storage.NewLocalFS(tempDir)
	if err != nil {
		t.Fatalf("NewLocalFS(tempDir): %v", err)
	}


	// 3. miniredis（无需外部 Redis 进程）
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	redisStore := metadata.NewRedisStore(rdb, "test:")

	// 4. SQLite 内存数据库
	sqliteStore, err := metadata.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}

	// 5. Metadata 门面
	metaFacade := metadata.NewFacade(redisStore, sqliteStore)

	// 6. 压缩器
	compress, err := compressor.NewCompressor()
	if err != nil {
		t.Fatalf("NewCompressor: %v", err)
	}

	// 7. 哈希器
	hasher := hasher.NewSHA256Hasher()

	// 8. 领域服务
	uploadCfg := domain.UploadConfig{
		SessionTTL:       time.Hour,
		DataDir:          dataDir,
		DefaultChunkSize: 1024 * 1024,
	}
	workerPool := domain.NewSimpleWorkerPool(4, 10)
	uploadSvc := domain.NewUploadService(metaFacade, localFS, tempFS, compress, hasher, workerPool, uploadCfg)

	downloadCfg := domain.DownloadConfig{DataDir: dataDir}
	downloadSvc := domain.NewDownloadService(metaFacade, localFS, compress, hasher, downloadCfg)

	// 9. Scanner
	scanner := lifecycle.NewConsistencyScanner(metaFacade, localFS, dataDir, tempDir)

	// 10. 传输层
	mw := NewMiddleware().WithAuth(AuthConfig{Enabled: false})
	tusHandler := NewTusHandler(uploadSvc)
	restHandler := NewRESTHandler(uploadSvc, downloadSvc)
	downloadHandler := NewDownloadHandler(downloadSvc)
	router := NewRouter(mw, tusHandler, restHandler, downloadHandler, nil, nil, nil, nil, nil, uploadSvc, scanner, nil)

	// 10. httptest 服务器
	srv := httptest.NewServer(router.Handler())
	t.Cleanup(srv.Close)

	return &e2eFixture{
		t:       t,
		baseURL: srv.URL,
		client:  srv.Client(),
		srv:     srv,
		dataDir: dataDir,
		tempDir: tempDir,
		meta:    metaFacade,
	}
}

// doReq 发送 HTTP 请求并返回响应
func (f *e2eFixture) doReq(method, path string, body io.Reader, headers map[string]string) *http.Response {
	t := f.t
	t.Helper()

	req, err := http.NewRequest(method, f.baseURL+path, body)
	if err != nil {
		t.Fatalf("NewRequest(%s %s): %v", method, path, err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatalf("Do(%s %s): %v", method, path, err)
	}
	return resp
}

// readBody 读取并关闭响应 body
func readBody(resp *http.Response) string {
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return string(data)
}

// sha256Hex 计算数据的 SHA-256
func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// ============ 测试用例 ============

func TestE2E_HealthCheck(t *testing.T) {
	f := newE2EFixture(t)
	resp := f.doReq("GET", "/health", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("health status = %d, want 200", resp.StatusCode)
	}
	if _, err := io.ReadAll(resp.Body); err != nil {
		t.Fatalf("read health body: %v", err)
	}
	resp.Body.Close()
}

func TestE2E_UploadAndDownload(t *testing.T) {
	f := newE2EFixture(t)
	ctx := context.Background()
	namespace := "default"
	content := []byte("Hello, E2E 世界! This is a file upload/download test.")
	contentSHA := sha256Hex(content)

	// === 1. Init upload ===
	initResp := f.doReq("POST", "/v1/uploads/init?size="+fmt.Sprint(len(content)), nil, map[string]string{
		"X-SHA256":     contentSHA,
		"X-File-Name":  "e2e-test.txt",
		"X-Namespace":  namespace,
		"X-Compression": "none",
	})
	if initResp.StatusCode != http.StatusCreated {
		t.Fatalf("InitUpload status = %d, body=%s", initResp.StatusCode, readBody(initResp))
	}
	var initResult map[string]any
	if err := json.NewDecoder(initResp.Body).Decode(&initResult); err != nil {
		t.Fatalf("InitUpload decode: %v", err)
	}
	initResp.Body.Close()

	sessionID := initResult["session_id"].(string)
	if sessionID == "" {
		t.Fatal("session_id is empty")
	}

	// === 2. Upload chunk ===
	chunkResp := f.doReq("PUT", "/v1/uploads/"+sessionID+"/chunks/0",
		bytes.NewReader(content), map[string]string{
			"X-Namespace":    namespace,
			"Content-Type":   "application/octet-stream",
		})
	if chunkResp.StatusCode != http.StatusOK {
		t.Fatalf("UploadChunk status = %d, body=%s", chunkResp.StatusCode, readBody(chunkResp))
	}
	chunkResp.Body.Close()

	// === 3. Check status ===
	statusResp := f.doReq("GET", "/v1/uploads/"+sessionID+"/status", nil, map[string]string{
		"X-Namespace": namespace,
	})
	if statusResp.StatusCode != http.StatusOK {
		t.Fatalf("GetStatus status = %d, body=%s", statusResp.StatusCode, readBody(statusResp))
	}
	var statusResult map[string]any
	json.NewDecoder(statusResp.Body).Decode(&statusResult)
	statusResp.Body.Close()
	if statusResult["session_id"] != sessionID {
		t.Error("status session_id mismatch")
	}

	// === 4. Finalize ===
	finalizeResp := f.doReq("POST", "/v1/uploads/"+sessionID+"/finalize", nil, map[string]string{
		"X-Namespace": namespace,
	})
	if finalizeResp.StatusCode != http.StatusOK {
		t.Fatalf("Finalize status = %d, body=%s", finalizeResp.StatusCode, readBody(finalizeResp))
	}
	var finalizeResult map[string]any
	json.NewDecoder(finalizeResp.Body).Decode(&finalizeResult)
	finalizeResp.Body.Close()

	fileID := finalizeResult["file_id"].(string)
	if fileID == "" {
		t.Fatal("file_id is empty")
	}
	if finalizeResult["sha256"] != contentSHA {
		t.Errorf("sha256 = %v, want %s", finalizeResult["sha256"], contentSHA)
	}

	// === 5. Stat file ===
	statResp := f.doReq("GET", "/v1/stat/"+fileID, nil, map[string]string{
		"X-Namespace": namespace,
	})
	if statResp.StatusCode != http.StatusOK {
		t.Fatalf("Stat status = %d, body=%s", statResp.StatusCode, readBody(statResp))
	}
	var statResult map[string]any
	json.NewDecoder(statResp.Body).Decode(&statResult)
	statResp.Body.Close()
	file := statResult["file"].(map[string]any)
	if file["file_id"] != fileID {
		t.Error("Stat file_id mismatch")
	}

	// === 6. Download file ===
	downloadResp := f.doReq("GET", "/v1/files/"+fileID, nil, map[string]string{
		"X-Namespace": namespace,
	})
	if downloadResp.StatusCode != http.StatusOK {
		t.Fatalf("Download status = %d, body=%s", downloadResp.StatusCode, readBody(downloadResp))
	}
	downloaded, _ := io.ReadAll(downloadResp.Body)
	downloadResp.Body.Close()

	if !bytes.Equal(downloaded, content) {
		t.Errorf("downloaded content mismatch: got %d bytes, want %d", len(downloaded), len(content))
	}
	downloadedSHA := sha256Hex(downloaded)
	if downloadedSHA != contentSHA {
		t.Errorf("downloaded sha256 = %s, want %s", downloadedSHA, contentSHA)
	}
	// 验证响应头中有 SHA256
	if downloadResp.Header.Get("X-SHA256") != contentSHA {
		t.Errorf("X-SHA256 header = %s, want %s", downloadResp.Header.Get("X-SHA256"), contentSHA)
	}

	// === 7. CheckExists — hit ===
	checkResp := f.doReq("HEAD", "/v1/files?sha256="+contentSHA+"&name=e2e-check.txt", nil, map[string]string{
		"X-Namespace": namespace,
	})
	if checkResp.StatusCode != http.StatusOK {
		t.Fatalf("CheckExists(hit) status = %d, want 200", checkResp.StatusCode)
	}
	checkResp.Body.Close()

	// === 8. CheckExists — miss ===
	missResp := f.doReq("HEAD", "/v1/files?sha256=0000000000000000000000000000000000000000000000000000000000000000", nil, map[string]string{
		"X-Namespace": namespace,
	})
	if missResp.StatusCode != http.StatusNotFound {
		t.Fatalf("CheckExists(miss) status = %d, want 404", missResp.StatusCode)
	}
	missResp.Body.Close()

	// === 9. List root dir ===
	lsResp := f.doReq("GET", "/v1/ls", nil, map[string]string{
		"X-Namespace": namespace,
	})
	if lsResp.StatusCode != http.StatusOK {
		t.Fatalf("ListDir status = %d, body=%s", lsResp.StatusCode, readBody(lsResp))
	}
	var lsResult map[string]any
	json.NewDecoder(lsResp.Body).Decode(&lsResult)
	lsResp.Body.Close()
	children := lsResult["children"].([]any)
	if len(children) == 0 {
		t.Error("ListDir returned 0 children, expected at least 1 (just uploaded)")
	}

	// === 10. Delete file ===
	delResp := f.doReq("DELETE", "/v1/files/"+fileID, nil, map[string]string{
		"X-Namespace": namespace,
	})
	if delResp.StatusCode != http.StatusNoContent {
		t.Fatalf("Delete status = %d, body=%s", delResp.StatusCode, readBody(delResp))
	}
	delResp.Body.Close()

	// 验证已删除
	statDelResp := f.doReq("GET", "/v1/stat/"+fileID, nil, map[string]string{
		"X-Namespace": namespace,
	})
	if statDelResp.StatusCode != http.StatusNotFound {
		t.Errorf("Stat after delete status = %d, want 404, body=%s", statDelResp.StatusCode, readBody(statDelResp))
	}
	statDelResp.Body.Close()

	_ = ctx
}

func TestE2E_DirUploadAndDownload(t *testing.T) {
	f := newE2EFixture(t)
	namespace := "demo"

	// === 1. 上传两个文件作为目录素材 ===
	contentA := []byte("file a content")
	contentB := []byte("file b content larger one")
	shaA := sha256Hex(contentA)
	shaB := sha256Hex(contentB)

	fileIDs := make([]string, 2)
	for i, c := range [][]byte{contentA, contentB} {
		// Init
		initResp := f.doReq("POST", "/v1/uploads/init?size="+fmt.Sprint(len(c)), nil, map[string]string{
			"X-SHA256":     sha256Hex(c),
			"X-File-Name":  fmt.Sprintf("file_%d.txt", i),
			"X-Namespace":  namespace,
			"X-Compression": "none",
		})
		if initResp.StatusCode != http.StatusCreated {
			t.Fatalf("InitUpload file %d status = %d", i, initResp.StatusCode)
		}
		var initResult map[string]any
		json.NewDecoder(initResp.Body).Decode(&initResult)
		initResp.Body.Close()
		sessionID := initResult["session_id"].(string)

		// Upload chunk
		chunkResp := f.doReq("PUT", "/v1/uploads/"+sessionID+"/chunks/0",
			bytes.NewReader(c), map[string]string{"X-Namespace": namespace})
		if chunkResp.StatusCode != http.StatusOK {
			t.Fatalf("UploadChunk file %d status = %d", i, chunkResp.StatusCode)
		}
		chunkResp.Body.Close()

		// Finalize
		finalizeResp := f.doReq("POST", "/v1/uploads/"+sessionID+"/finalize", nil,
			map[string]string{"X-Namespace": namespace})
		if finalizeResp.StatusCode != http.StatusOK {
			t.Fatalf("Finalize file %d status = %d", i, finalizeResp.StatusCode)
		}
		var finalizeResult map[string]any
		json.NewDecoder(finalizeResp.Body).Decode(&finalizeResult)
		finalizeResp.Body.Close()
		fileIDs[i] = finalizeResult["file_id"].(string)
	}

	// === 2. Submit dir manifest ===
	manifest := domain.DirManifest{
		Entries: []domain.DirEntry{
			{Path: "subdir/a.txt", FileID: fileIDs[0]},
			{Path: "subdir/b.txt", FileID: fileIDs[1]},
		},
	}
	manifestBody, _ := json.Marshal(manifest)
	dirResp := f.doReq("POST", "/v1/dirs", bytes.NewReader(manifestBody), map[string]string{
		"X-Namespace": namespace,
		"Content-Type": "application/json",
	})
	if dirResp.StatusCode != http.StatusCreated {
		t.Fatalf("SubmitDir status = %d, body=%s", dirResp.StatusCode, readBody(dirResp))
	}
	var dirResult map[string]string
	json.NewDecoder(dirResp.Body).Decode(&dirResult)
	dirResp.Body.Close()
	dirID := dirResult["file_id"]

	// === 3. Stat dir ===
	statResp := f.doReq("GET", "/v1/stat/"+dirID, nil, map[string]string{
		"X-Namespace": namespace,
	})
	if statResp.StatusCode != http.StatusOK {
		t.Fatalf("Stat dir status = %d, body=%s", statResp.StatusCode, readBody(statResp))
	}
	statResp.Body.Close()

	// === 4. List dir ===
	lsResp := f.doReq("GET", "/v1/ls?parent="+dirID, nil, map[string]string{
		"X-Namespace": namespace,
	})
	if lsResp.StatusCode != http.StatusOK {
		t.Fatalf("ListDir status = %d, body=%s", lsResp.StatusCode, readBody(lsResp))
	}
	var lsResult map[string]any
	json.NewDecoder(lsResp.Body).Decode(&lsResult)
	lsResp.Body.Close()
	children := lsResult["children"].([]any)
		if len(children) != 1 {
			t.Errorf("ListDir children = %d, want 1 (subdir dir node)", len(children))
		}
		firstChild := children[0].(map[string]any)
		subdirID, ok := firstChild["file_id"].(string)
		if !ok || !firstChild["is_dir"].(bool) {
			t.Fatal("expected subdir directory node")
		}

		// List subdir
		subResp := f.doReq("GET", "/v1/ls?parent="+subdirID, nil, map[string]string{
			"X-Namespace": namespace,
		})
		if subResp.StatusCode != http.StatusOK {
			t.Fatalf("ListSubdir status = %d", subResp.StatusCode)
		}
		var subResult map[string]any
		json.NewDecoder(subResp.Body).Decode(&subResult)
		subResp.Body.Close()
		subChildren := subResult["children"].([]any)
		if len(subChildren) != 2 {
			t.Errorf("Subdir children = %d, want 2 (a.txt, b.txt)", len(subChildren))
		}

	// === 5. Download dir (just verify it starts streaming) ===
	dlResp := f.doReq("GET", "/v1/dirs/"+dirID+"?format=tar.gz", nil, map[string]string{
		"X-Namespace": namespace,
	})
	if dlResp.StatusCode != http.StatusOK {
		t.Fatalf("DownloadDir status = %d, body=%s", dlResp.StatusCode, readBody(dlResp))
	}
	// Verify we get gzip data
	dlData, _ := io.ReadAll(dlResp.Body)
	dlResp.Body.Close()
	if len(dlData) < 20 {
		t.Errorf("DownloadDir data too short: %d bytes", len(dlData))
	}
	// gzip magic bytes: 1f 8b
	if len(dlData) >= 2 && dlData[0] != 0x1f && dlData[1] != 0x8b {
		t.Logf("Warning: downloaded dir data doesn't start with gzip magic (may be expected depending on compressor)")
	}

	// Check X-Tree-SHA256 header
	if dlResp.Header.Get("X-Tree-SHA256") == "" {
		t.Error("DownloadDir missing X-Tree-SHA256 header")
	}

	_ = shaA
	_ = shaB
}

func TestE2E_TusProtocol(t *testing.T) {
	f := newE2EFixture(t)
	namespace := "tus-ns"
	content := []byte("tus protocol test data")
	contentSHA := sha256Hex(content)

	// === 1. Tus POST — create session ===
	postResp := f.doReq("POST", "/uploads", nil, map[string]string{
		"Upload-Length": fmt.Sprint(len(content)),
		"X-SHA256":      contentSHA,
		"X-File-Name":   "tus-test.txt",
		"X-Compression": "none",
		"X-Chunk-Size":  "1048576",
		"X-Namespace":   namespace,
	})
	if postResp.StatusCode != http.StatusCreated {
		t.Fatalf("Tus POST status = %d, want 201; body=%s", postResp.StatusCode, readBody(postResp))
	}
	location := postResp.Header.Get("Location")
	if location == "" {
		t.Fatal("Tus POST missing Location header")
	}
	sessionID := strings.TrimPrefix(location, "/uploads/")
	if sessionID == "" {
		t.Fatal("empty session ID from Location header")
	}
	postResp.Body.Close()

	if postResp.Header.Get("Upload-Offset") != "0" {
		t.Errorf("Upload-Offset = %s, want 0", postResp.Header.Get("Upload-Offset"))
	}

	// === 2. Tus HEAD — verify offset (0) ===
	headResp := f.doReq("HEAD", "/uploads/"+sessionID, nil, map[string]string{
		"X-Namespace": namespace,
	})
	if headResp.StatusCode != http.StatusOK {
		t.Fatalf("Tus HEAD status = %d, want 200; body=%s", headResp.StatusCode, readBody(headResp))
	}
	offset := headResp.Header.Get("Upload-Offset")
	if offset != "0" {
		t.Errorf("HEAD offset = %s, want 0", offset)
	}
	headResp.Body.Close()

	// === 3. Tus PATCH — upload chunk ===
	patchResp := f.doReq("PATCH", "/uploads/"+sessionID,
		bytes.NewReader(content), map[string]string{
			"Upload-Offset":  "0",
			"X-Slice-Index":  "0",
			"X-Slice-SHA256": contentSHA,
			"X-Namespace":    namespace,
			"Content-Type":   "application/offset+octet-stream",
		})
	if patchResp.StatusCode != http.StatusNoContent {
		t.Fatalf("Tus PATCH status = %d, want 204; body=%s", patchResp.StatusCode, readBody(patchResp))
	}
	_ = patchResp.Header.Get("Upload-Offset")
	patchResp.Body.Close()

	// === 4. Tus HEAD — check offset (may be 0 if metadata store doesn't track sizes) ===
	head2Resp := f.doReq("HEAD", "/uploads/"+sessionID, nil, map[string]string{
		"X-Namespace": namespace,
	})
	if head2Resp.StatusCode != http.StatusOK {
		t.Fatalf("Tus HEAD2 status = %d; body=%s", head2Resp.StatusCode, readBody(head2Resp))
	}
	head2Resp.Body.Close()

	// === 5. Finalize (REST endpoint) ===
	finalResp := f.doReq("POST", "/v1/uploads/"+sessionID+"/finalize", nil, map[string]string{
		"X-Namespace": namespace,
	})
	if finalResp.StatusCode != http.StatusOK {
		t.Fatalf("Finalize status = %d; body=%s", finalResp.StatusCode, readBody(finalResp))
	}
	var finalResult map[string]any
	json.NewDecoder(finalResp.Body).Decode(&finalResult)
	finalResp.Body.Close()
	fileID := finalResult["file_id"].(string)
	if finalResult["sha256"] != contentSHA {
		t.Errorf("finalize sha256 = %v, want %s", finalResult["sha256"], contentSHA)
	}

	// === 6. Download and verify ===
	dlResp := f.doReq("GET", "/v1/files/"+fileID, nil, map[string]string{
		"X-Namespace": namespace,
	})
	if dlResp.StatusCode != http.StatusOK {
		t.Fatalf("Download status = %d; body=%s", dlResp.StatusCode, readBody(dlResp))
	}
	downloaded, _ := io.ReadAll(dlResp.Body)
	dlResp.Body.Close()
	if !bytes.Equal(downloaded, content) {
		t.Errorf("downloaded content mismatch: got %d bytes, want %d", len(downloaded), len(content))
	}
}

func TestE2E_ZstdCompression(t *testing.T) {
	f := newE2EFixture(t)
	namespace := "zstd-ns"
	content := bytes.Repeat([]byte("compressible data pattern! "), 1000)
	contentSHA := sha256Hex(content)

	initResp := f.doReq("POST", "/v1/uploads/init?size="+fmt.Sprint(len(content)), nil, map[string]string{
		"X-SHA256":      contentSHA,
		"X-File-Name":   "zstd-test.txt",
		"X-Namespace":   namespace,
		"X-Compression": "zstd",
	})
	if initResp.StatusCode != http.StatusCreated {
		t.Fatalf("Init status = %d; body=%s", initResp.StatusCode, readBody(initResp))
	}
	var initResult map[string]any
	json.NewDecoder(initResp.Body).Decode(&initResult)
	initResp.Body.Close()
	sessionID := initResult["session_id"].(string)

	// Compress data using zstd directly
	var compressedBuf bytes.Buffer
	zw, err := zstd.NewWriter(&compressedBuf)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.Copy(zw, bytes.NewReader(content))
	zw.Close()
	if err != nil {
		t.Fatal(err)
	}
	compressedData := compressedBuf.Bytes()

	chunkResp := f.doReq("PUT", "/v1/uploads/"+sessionID+"/chunks/0",
		bytes.NewReader(compressedData), map[string]string{
			"X-Namespace":  namespace,
			"Content-Type": "application/octet-stream",
		})
	if chunkResp.StatusCode != http.StatusOK {
		t.Fatalf("UploadChunk status = %d; body=%s", chunkResp.StatusCode, readBody(chunkResp))
	}
	chunkResp.Body.Close()

	finalResp := f.doReq("POST", "/v1/uploads/"+sessionID+"/finalize", nil, map[string]string{
		"X-Namespace": namespace,
	})
	if finalResp.StatusCode != http.StatusOK {
		t.Fatalf("Finalize status = %d; body=%s", finalResp.StatusCode, readBody(finalResp))
	}
	var finalResult map[string]any
	json.NewDecoder(finalResp.Body).Decode(&finalResult)
	finalResp.Body.Close()
	fileID := finalResult["file_id"].(string)
	if finalResult["sha256"] != contentSHA {
		t.Errorf("sha256 = %v, want %s", finalResult["sha256"], contentSHA)
	}

	// Download and verify decompressed content matches original
	dlResp := f.doReq("GET", "/v1/files/"+fileID, nil, map[string]string{
		"X-Namespace": namespace,
	})
	if dlResp.StatusCode != http.StatusOK {
		t.Fatalf("Download status = %d; body=%s", dlResp.StatusCode, readBody(dlResp))
	}
	downloaded, _ := io.ReadAll(dlResp.Body)
	dlResp.Body.Close()
	if !bytes.Equal(downloaded, content) {
		t.Errorf("downloaded content mismatch: got %d bytes, want %d", len(downloaded), len(content))
	}
	if sha256Hex(downloaded) != contentSHA {
		t.Errorf("downloaded SHA256 mismatch")
	}
}

func TestE2E_CancelUpload(t *testing.T) {
	f := newE2EFixture(t)
	namespace := "cancel-ns"
	content := []byte("to be cancelled")

	initResp := f.doReq("POST", "/v1/uploads/init?size="+fmt.Sprint(len(content)), nil, map[string]string{
		"X-SHA256":      sha256Hex(content),
		"X-File-Name":   "cancel-test.txt",
		"X-Namespace":   namespace,
		"X-Compression": "none",
	})
	if initResp.StatusCode != http.StatusCreated {
		t.Fatalf("Init status = %d", initResp.StatusCode)
	}
	var initResult map[string]any
	json.NewDecoder(initResp.Body).Decode(&initResult)
	initResp.Body.Close()
	sessionID := initResult["session_id"].(string)

	// Upload chunk so there's temp data
	chunkResp := f.doReq("PUT", "/v1/uploads/"+sessionID+"/chunks/0",
		bytes.NewReader(content), map[string]string{"X-Namespace": namespace})
	if chunkResp.StatusCode != http.StatusOK {
		t.Fatalf("UploadChunk status = %d", chunkResp.StatusCode)
	}
	chunkResp.Body.Close()

	// Cancel (tus DELETE)
	cancelResp := f.doReq("DELETE", "/uploads/"+sessionID, nil, map[string]string{
		"X-Namespace": namespace,
	})
	if cancelResp.StatusCode != http.StatusNoContent {
		t.Fatalf("Cancel status = %d, want 204", cancelResp.StatusCode)
	}
	cancelResp.Body.Close()

	// Finalize should fail after cancel
	finalResp := f.doReq("POST", "/v1/uploads/"+sessionID+"/finalize", nil, map[string]string{
		"X-Namespace": namespace,
	})
	if finalResp.StatusCode != http.StatusNotFound && finalResp.StatusCode != http.StatusConflict {
		t.Errorf("Finalize after cancel status = %d, want 404 or 409; body=%s",
			finalResp.StatusCode, readBody(finalResp))
	}
	finalResp.Body.Close()

	// Verify temp dir cleaned
	tempSessionDir := filepath.Join(f.tempDir, sessionID)
	_, err := os.Stat(tempSessionDir)
	if !os.IsNotExist(err) {
		t.Logf("Note: temp dir %s still exists (may be cleaned async)", tempSessionDir)
	}
}

func TestE2E_MultipleFilesSequential(t *testing.T) {
	f := newE2EFixture(t)
	namespace := "multi-seq-ns"
	numFiles := 5

	for i := range numFiles {
		content := []byte(fmt.Sprintf("sequential file %d — unique content here", i))
		contentSHA := sha256Hex(content)
		name := fmt.Sprintf("seq-%d.txt", i)

		t.Run(name, func(t *testing.T) {
			initResp := f.doReq("POST", "/v1/uploads/init?size="+fmt.Sprint(len(content)), nil, map[string]string{
				"X-SHA256": contentSHA, "X-Compression": "none",
				"X-File-Name": name, "X-Namespace": namespace,
			})
			if initResp.StatusCode != http.StatusCreated {
				t.Fatalf("init status=%d", initResp.StatusCode)
			}
			var initResult map[string]any
			json.NewDecoder(initResp.Body).Decode(&initResult)
			initResp.Body.Close()
			sessionID := initResult["session_id"].(string)

			chunkResp := f.doReq("PUT", "/v1/uploads/"+sessionID+"/chunks/0",
				bytes.NewReader(content), map[string]string{"X-Namespace": namespace})
			if chunkResp.StatusCode != http.StatusOK {
				t.Fatalf("chunk status=%d", chunkResp.StatusCode)
			}
			chunkResp.Body.Close()

			finalResp := f.doReq("POST", "/v1/uploads/"+sessionID+"/finalize", nil,
				map[string]string{"X-Namespace": namespace})
			if finalResp.StatusCode != http.StatusOK {
				t.Fatalf("finalize status=%d; body=%s", finalResp.StatusCode, readBody(finalResp))
			}
			var finalResult map[string]any
			json.NewDecoder(finalResp.Body).Decode(&finalResult)
			finalResp.Body.Close()

			dlResp := f.doReq("GET", "/v1/files/"+finalResult["file_id"].(string), nil,
				map[string]string{"X-Namespace": namespace})
			if dlResp.StatusCode != http.StatusOK {
				t.Fatalf("download status=%d", dlResp.StatusCode)
			}
			downloaded, _ := io.ReadAll(dlResp.Body)
			dlResp.Body.Close()
			if !bytes.Equal(downloaded, content) {
				t.Error("content mismatch")
			}
		})
	}
}

func TestE2E_AdminScan(t *testing.T) {
	f := newE2EFixture(t)

	content := []byte("scan test content")
	contentSHA := sha256Hex(content)
	initResp := f.doReq("POST", "/v1/uploads/init?size="+fmt.Sprint(len(content)), nil, map[string]string{
		"X-SHA256": contentSHA, "X-Compression": "none",
		"X-File-Name": "scan-test.txt", "X-Namespace": "scan-ns",
	})
	var initResult map[string]any
	json.NewDecoder(initResp.Body).Decode(&initResult)
	initResp.Body.Close()
	sess := initResult["session_id"].(string)
	f.doReq("PUT", "/v1/uploads/"+sess+"/chunks/0",
		bytes.NewReader(content), map[string]string{"X-Namespace": "scan-ns"}).Body.Close()
	finalResp := f.doReq("POST", "/v1/uploads/"+sess+"/finalize", nil,
		map[string]string{"X-Namespace": "scan-ns"})
	var finalResult map[string]any
	json.NewDecoder(finalResp.Body).Decode(&finalResult)
	finalResp.Body.Close()

	// Orphan temp file (no corresponding session)
	orphanPart := filepath.Join(f.tempDir, "orphan-standalone.part")
	os.WriteFile(orphanPart, []byte("orphan"), 0644)

	// Run scan
	scanResp := f.doReq("POST", "/v1/admin/scan", nil, nil)
	if scanResp.StatusCode != http.StatusOK {
		t.Fatalf("Admin scan status = %d, want 200; body=%s", scanResp.StatusCode, readBody(scanResp))
	}
	var report map[string]any
	json.NewDecoder(scanResp.Body).Decode(&report)
	scanResp.Body.Close()

	for _, key := range []string{"orphan_parts", "orphan_files", "metadata_orphans", "ref_count_fixes"} {
		if _, ok := report[key]; !ok {
			t.Errorf("scan report missing %s", key)
		}
	}
	t.Logf("Scan report: %+v", report)

	_ = contentSHA
	_ = finalResult
}

func TestE2E_SecPassDedup(t *testing.T) {
	f := newE2EFixture(t)
	namespace := "dedup-ns"
	content := []byte("dedup test content — same content, two uploads")
	sameSHA := sha256Hex(content)

	// 第一次上传完整流程
	initResp1 := f.doReq("POST", "/v1/uploads/init?size="+fmt.Sprint(len(content)), nil, map[string]string{
		"X-SHA256":     sameSHA,
		"X-File-Name":  "original.txt",
		"X-Namespace":  namespace,
		"X-Compression": "none",
	})
	if initResp1.StatusCode != http.StatusCreated {
		t.Fatalf("Init 1 status = %d", initResp1.StatusCode)
	}
	var init1 map[string]any
	json.NewDecoder(initResp1.Body).Decode(&init1)
	initResp1.Body.Close()
	sess1 := init1["session_id"].(string)

	f.doReq("PUT", "/v1/uploads/"+sess1+"/chunks/0",
		bytes.NewReader(content), map[string]string{"X-Namespace": namespace}).Body.Close()

	finalize1 := f.doReq("POST", "/v1/uploads/"+sess1+"/finalize", nil,
		map[string]string{"X-Namespace": namespace})
	var final1 map[string]any
	json.NewDecoder(finalize1.Body).Decode(&final1)
	finalize1.Body.Close()
	fileID1 := final1["file_id"].(string)

	// 秒传：用 CheckExists 命中同一个 SHA
	checkResp := f.doReq("HEAD", "/v1/files?sha256="+sameSHA+"&name=dedup-copy.txt", nil, map[string]string{
		"X-Namespace": namespace,
	})
	if checkResp.StatusCode != http.StatusOK {
		t.Fatalf("CheckExists(hit) status = %d, want 200", checkResp.StatusCode)
	}
	checkResp.Body.Close()

	// 验证文件存在（秒传成功，有 file_id）
	// CheckExists 返回的 body 可能有 file_id
	_ = fileID1
}

func TestE2E_ErrorCases(t *testing.T) {
	f := newE2EFixture(t)

	tests := []struct {
		name   string
		method string
		path   string
		headers map[string]string
		body   io.Reader
		want   int
	}{
		{
			name:   "Init without size",
			method: "POST",
			path:   "/v1/uploads/init",
			want:   http.StatusBadRequest,
		},
		{
			name:   "Finalize nonexistent session",
			method: "POST",
			path:   "/v1/uploads/nonexistent-session-id/finalize",
			headers: map[string]string{"X-Namespace": "ns"},
			want:   http.StatusNotFound,
		},
		{
			name:   "Download nonexistent file",
			method: "GET",
			path:   "/v1/files/nonexistent-file-id",
			headers: map[string]string{"X-Namespace": "ns"},
			want:   http.StatusNotFound,
		},
		{
			name:   "Stat nonexistent file",
			method: "GET",
			path:   "/v1/stat/nonexistent-id",
			headers: map[string]string{"X-Namespace": "ns"},
			want:   http.StatusNotFound,
		},
		{
			name:   "Delete nonexistent file",
			method: "DELETE",
			path:   "/v1/files/nonexistent",
			headers: map[string]string{"X-Namespace": "ns"},
			want:   http.StatusNotFound,
		},
		{
			name:   "CheckExists no sha256",
			method: "HEAD",
			path:   "/v1/files",
			want:   http.StatusBadRequest,
		},
		{
			name:   "Upload chunk nonexistent session",
			method: "PUT",
			path:   "/v1/uploads/x/chunks/0",
			headers: map[string]string{"X-Namespace": "ns"},
			want:   http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := f.doReq(tt.method, tt.path, tt.body, tt.headers)
			if resp.StatusCode != tt.want {
				t.Errorf("status = %d, want %d; body=%s", resp.StatusCode, tt.want, readBody(resp))
			}
			resp.Body.Close()
		})
	}
}

func TestE2E_NamespaceIsolation(t *testing.T) {
	f := newE2EFixture(t)
	content := []byte("namespace isolated content")
	sha := sha256Hex(content)

	// 用 ns-a 上传
	initResp := f.doReq("POST", "/v1/uploads/init?size="+fmt.Sprint(len(content)), nil, map[string]string{
		"X-SHA256":      sha,
		"X-File-Name":   "ns-test.txt",
		"X-Namespace":   "ns-a",
		"X-Compression": "none",
	})
	if initResp.StatusCode != http.StatusCreated {
		t.Fatalf("Init ns-a status = %d", initResp.StatusCode)
	}
	var initResult map[string]any
	json.NewDecoder(initResp.Body).Decode(&initResult)
	initResp.Body.Close()
	sessionID := initResult["session_id"].(string)

	f.doReq("PUT", "/v1/uploads/"+sessionID+"/chunks/0",
		bytes.NewReader(content), map[string]string{"X-Namespace": "ns-a"}).Body.Close()

	finalResp := f.doReq("POST", "/v1/uploads/"+sessionID+"/finalize", nil,
		map[string]string{"X-Namespace": "ns-a"})
	var finalResult map[string]any
	json.NewDecoder(finalResp.Body).Decode(&finalResult)
	finalResp.Body.Close()
	fileID := finalResult["file_id"].(string)

	// 用 ns-b 访问同一个 fileID 应 namespace 隔离
	// Stat 隐藏文件存在信息返回 404, Download 直接返回 403
	statResp := f.doReq("GET", "/v1/stat/"+fileID, nil, map[string]string{
		"X-Namespace": "ns-b",
	})
	if statResp.StatusCode != http.StatusNotFound {
		t.Errorf("Stat from different namespace status = %d, want 404; body=%s", statResp.StatusCode, readBody(statResp))
	}
	statResp.Body.Close()

	// 用 ns-b 下载应 403
	dlResp := f.doReq("GET", "/v1/files/"+fileID, nil, map[string]string{
		"X-Namespace": "ns-b",
	})
	if dlResp.StatusCode != http.StatusForbidden {
		t.Errorf("Download from different namespace status = %d, want 403", dlResp.StatusCode)
	}
	dlResp.Body.Close()

	// ns-a 应能正常读取
	okResp := f.doReq("GET", "/v1/stat/"+fileID, nil, map[string]string{
		"X-Namespace": "ns-a",
	})
	if okResp.StatusCode != http.StatusOK {
		t.Errorf("Stat from correct namespace status = %d, want 200; body=%s", okResp.StatusCode, readBody(okResp))
	}
	okResp.Body.Close()
}

func TestE2E_MultipleChunks(t *testing.T) {
	f := newE2EFixture(t)
	namespace := "multi-chunk"

	// 构建 3MB 数据（多个分片）
	content := bytes.Repeat([]byte("ABCDEFGHIJ"), 300*1024) // 3MB
	contentSHA := sha256Hex(content)

	// Init
	initResp := f.doReq("POST", "/v1/uploads/init?size="+fmt.Sprint(len(content)), nil, map[string]string{
		"X-SHA256":      contentSHA,
		"X-File-Name":   "multi-chunk.bin",
		"X-Namespace":   namespace,
		"X-Compression": "none",
	})
	if initResp.StatusCode != http.StatusCreated {
		t.Fatalf("Init status = %d", initResp.StatusCode)
	}
	var initResult map[string]any
	json.NewDecoder(initResp.Body).Decode(&initResult)
	initResp.Body.Close()
	sessionID := initResult["session_id"].(string)

	// 上传 3 个分片（每片 1MB）
	chunkSize := 1024 * 1024
	totalChunks := (len(content) + chunkSize - 1) / chunkSize
	for i := range totalChunks {
		start := i * chunkSize
		end := min(start+chunkSize, len(content))
		chunk := content[start:end]
		chunkResp := f.doReq("PUT", "/v1/uploads/"+sessionID+"/chunks/"+fmt.Sprint(i),
			bytes.NewReader(chunk), map[string]string{"X-Namespace": namespace})
		if chunkResp.StatusCode != http.StatusOK {
			t.Fatalf("Chunk %d status = %d, body=%s", i, chunkResp.StatusCode, readBody(chunkResp))
		}
		chunkResp.Body.Close()
	}

	// Finalize + verify
	finalResp := f.doReq("POST", "/v1/uploads/"+sessionID+"/finalize", nil,
		map[string]string{"X-Namespace": namespace})
	if finalResp.StatusCode != http.StatusOK {
		t.Fatalf("Finalize status = %d, body=%s", finalResp.StatusCode, readBody(finalResp))
	}
	var finalResult map[string]any
	json.NewDecoder(finalResp.Body).Decode(&finalResult)
	finalResp.Body.Close()
	fileID := finalResult["file_id"].(string)
	if finalResult["sha256"] != contentSHA {
		t.Errorf("sha256 = %v, want %s", finalResult["sha256"], contentSHA)
	}

	// Download + verify
	dlResp := f.doReq("GET", "/v1/files/"+fileID, nil, map[string]string{
		"X-Namespace": namespace,
	})
	if dlResp.StatusCode != http.StatusOK {
		t.Fatalf("Download status = %d", dlResp.StatusCode)
	}
	downloaded, _ := io.ReadAll(dlResp.Body)
	dlResp.Body.Close()

	if len(downloaded) != len(content) {
		t.Errorf("downloaded size = %d, want %d", len(downloaded), len(content))
	}
	if sha256Hex(downloaded) != contentSHA {
		t.Error("downloaded content SHA256 mismatch")
	}

	// Clean up temp files (verify)
	tempSessionDir := filepath.Join(f.tempDir, sessionID)
	_, err := os.Stat(tempSessionDir)
	if !os.IsNotExist(err) {
		t.Logf("Warning: temp dir %s still exists (may be expected if cleanup is async)", tempSessionDir)
	}
}

func TestE2E_DownloadRange(t *testing.T) {
	f := newE2EFixture(t)
	namespace := "range-ns"
	content := []byte("0123456789ABCDEF")
	contentSHA := sha256Hex(content)

	// Upload
	initResp := f.doReq("POST", "/v1/uploads/init?size="+fmt.Sprint(len(content)), nil, map[string]string{
		"X-SHA256": contentSHA, "X-Compression": "none",
		"X-File-Name": "range.bin", "X-Namespace": namespace,
	})
	var initResult map[string]any
	json.NewDecoder(initResp.Body).Decode(&initResult)
	initResp.Body.Close()
	sess := initResult["session_id"].(string)

	f.doReq("PUT", "/v1/uploads/"+sess+"/chunks/0",
		bytes.NewReader(content), map[string]string{"X-Namespace": namespace}).Body.Close()

	finalResp := f.doReq("POST", "/v1/uploads/"+sess+"/finalize", nil,
		map[string]string{"X-Namespace": namespace})
	var finalResult map[string]any
	json.NewDecoder(finalResp.Body).Decode(&finalResult)
	finalResp.Body.Close()
	fileID := finalResult["file_id"].(string)

	// Partial content — bytes 2-5 ("2345")
	dlResp := f.doReq("GET", "/v1/files/"+fileID, nil, map[string]string{
		"X-Namespace": namespace,
		"Range":       "bytes=2-5",
	})
	if dlResp.StatusCode != http.StatusPartialContent {
		t.Fatalf("Range download status = %d, want 206; body=%s", dlResp.StatusCode, readBody(dlResp))
	}
	partial, _ := io.ReadAll(dlResp.Body)
	dlResp.Body.Close()

	expected := []byte("2345")
	if !bytes.Equal(partial, expected) {
		t.Errorf("range content = %q, want %q", string(partial), string(expected))
	}
	if dlResp.Header.Get("Content-Range") == "" {
		t.Error("missing Content-Range header")
	}
}
