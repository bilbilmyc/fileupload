package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const defaultNamespace = "default"

// Client 统一的文件上传下载 HTTP 客户端。
type Client struct {
	ServerURL  string
	Namespace  string
	HTTPClient *http.Client
}

// NewClient 创建客户端实例。
func NewClient(serverURL, namespace string) *Client {
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}
	if namespace == "" {
		namespace = defaultNamespace
	}
	return &Client{
		ServerURL:  strings.TrimRight(serverURL, "/"),
		Namespace:  namespace,
		HTTPClient: &http.Client{Timeout: 60 * time.Second},
	}
}

// do 执行 HTTP 请求，对 5xx/503 自动重试最多 3 次，支持 Retry-After。
func (c *Client) do(req *http.Request) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		// 重试时重建 body：http.Client.Do 消费后 body 不能重用
		if attempt > 0 && req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			req.Body = body
		}
		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
			continue
		}
		if resp.StatusCode != http.StatusServiceUnavailable && resp.StatusCode < 500 {
			return resp, nil
		}
		ra := resp.Header.Get("Retry-After")
		resp.Body.Close()
		delay := time.Duration(attempt+1) * 500 * time.Millisecond
		if sec, err := strconv.Atoi(ra); err == nil && sec > 0 {
			delay = time.Duration(sec) * time.Second
		}
		time.Sleep(delay)
	}
	return nil, lastErr
}

// ListResult 目录列表结果。
type ListResult struct {
	Dir      any              `json:"dir"`
	Children []map[string]any `json:"children"`
}

// List 列出目录内容。
func (c *Client) List(ctx context.Context, parentID string) (*ListResult, error) {
	u := fmt.Sprintf("%s/v1/ls?parent=%s&namespace=%s", c.ServerURL, url.QueryEscape(parentID), url.QueryEscape(c.Namespace))
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list failed (%d): %s", resp.StatusCode, string(body))
	}
	var res ListResult
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	return &res, nil
}

// StatResult stat 查询结果。
type StatResult struct {
	File map[string]any `json:"file"`
	Blob map[string]any `json:"blob"`
}

// Stat 查询文件或目录信息。
func (c *Client) Stat(ctx context.Context, id string) (*StatResult, error) {
	u := fmt.Sprintf("%s/v1/stat/%s?namespace=%s", c.ServerURL, url.PathEscape(id), url.QueryEscape(c.Namespace))
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("stat failed (%d): %s", resp.StatusCode, string(body))
	}
	var res StatResult
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	return &res, nil
}

// Delete 删除文件。
func (c *Client) Delete(ctx context.Context, id string) error {
	u := fmt.Sprintf("%s/v1/files/%s?namespace=%s", c.ServerURL, url.PathEscape(id), url.QueryEscape(c.Namespace))
	req, _ := http.NewRequestWithContext(ctx, "DELETE", u, nil)
	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete failed (%d): %s", resp.StatusCode, string(body))
	}
	return nil
}

// DeleteDir 删除目录。
func (c *Client) DeleteDir(ctx context.Context, id string) error {
	u := fmt.Sprintf("%s/v1/dirs/%s?namespace=%s", c.ServerURL, url.PathEscape(id), url.QueryEscape(c.Namespace))
	req, _ := http.NewRequestWithContext(ctx, "DELETE", u, nil)
	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete dir failed (%d): %s", resp.StatusCode, string(body))
	}
	return nil
}

// Scan 触发服务端一致性巡检。
func (c *Client) Scan(ctx context.Context) (map[string]any, error) {
	u := fmt.Sprintf("%s/v1/admin/scan", c.ServerURL)
	req, _ := http.NewRequestWithContext(ctx, "POST", u, nil)
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("scan failed (%d): %s", resp.StatusCode, string(body))
	}
	var res map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	return res, nil
}

// resumeState 存储断点续传的暂存信息。
type resumeState struct {
	SessionID string `json:"session_id"`
	SHA256    string `json:"sha256"`
	FileSize  int64  `json:"file_size"`
}

// resumeStatePath 根据文件 SHA-256 返回 resume 状态文件路径。
func resumeStatePath(sha256 string) string {
	return filepath.Join(os.TempDir(), fmt.Sprintf(".fileupload.resume.%s.json", sha256))
}

// saveResumeState 保存 resume 状态到硬盘。
func saveResumeState(path string, state resumeState) error {
	data, _ := json.Marshal(state)
	return os.WriteFile(path, data, 0600)
}

// loadResumeState 从硬盘加载 resume 状态。
func loadResumeState(path string) (resumeState, error) {
	var state resumeState
	data, err := os.ReadFile(path)
	if err != nil {
		return state, err
	}
	err = json.Unmarshal(data, &state)
	return state, err
}

// UploadOptions 上传选项。
type UploadOptions struct {
	ChunkSize   int64
	Concurrency int
	Compress    string
	Resume      bool
	// FileName 自定义存储文件名。为空时使用本地文件的基本名。
	// 目录上传时设为相对路径以保留目录结构（如 "subdir/photo.jpg"）。
	FileName string
}

// UploadFile 上传单个文件。支持压缩、并发分片、秒传。
func (c *Client) UploadFile(ctx context.Context, localPath string, opts UploadOptions) (*FileInfo, error) {
	f, err := os.Open(localPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	// 使用自定义文件名（含相对路径）或默认基本名
	fileName := opts.FileName
	if fileName == "" {
		fileName = info.Name()
	}
	fileSize := info.Size()

	originalSHA, err := fileSHA256(localPath)
	if err != nil {
		return nil, err
	}

	if opts.Compress == "zstd" && fileSize > 0 {
		data, err := io.ReadAll(f)
		if err != nil {
			return nil, err
		}
		compressed, err := compressBuffer(data, "zstd")
		if err != nil {
			return nil, err
		}
		return c.uploadBytes(ctx, fileName, compressed, originalSHA, "zstd", opts)
	}

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	return c.uploadBytes(ctx, fileName, data, originalSHA, "none", opts)
}

// uploadBytes 将内存中的字节上传到服务端，含秒传、并发分片。
func (c *Client) uploadBytes(ctx context.Context, fileName string, data []byte, originalSHA, compress string, opts UploadOptions) (*FileInfo, error) {
	exists, err := c.CheckExists(ctx, originalSHA, fileName)
	if err != nil {
		return nil, err
	}
	if exists != nil {
		return exists, nil
	}

	if opts.ChunkSize <= 0 {
		opts.ChunkSize = 10 * 1024 * 1024
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 4
	}

	sessionID, err := c.CreateSession(ctx, int64(len(data)), originalSHA, compress, opts.ChunkSize, fileName)
	if err != nil {
		return nil, err
	}

	total := len(data)
	chunkCount := total / int(opts.ChunkSize)
	if total%int(opts.ChunkSize) != 0 {
		chunkCount++
	}

	// 保存 resume state（仅非 zstd 压缩，因为压缩后数据为临时数据）
	if opts.Resume && compress != "zstd" {
		saveResumeState(resumeStatePath(originalSHA), resumeState{
			SessionID: sessionID,
			SHA256:    originalSHA,
			FileSize:  int64(total),
		})
	}

	// 进度跟踪 — 可视化进度条（含实时速率）
	p := NewProgress(int64(total), "Uploading")
	p.Start()
	defer p.Stop()

	sem := make(chan struct{}, opts.Concurrency)
	errCh := make(chan error, chunkCount)

	for i := 0; i < chunkCount; i++ {
		start := i * int(opts.ChunkSize)
		end := start + int(opts.ChunkSize)
		if end > total {
			end = total
		}
		idx := i
		offset := int64(start)
		chunk := data[start:end]

		sem <- struct{}{}
		go func() {
			defer func() { <-sem }()
			err := c.UploadChunk(ctx, sessionID, idx, chunk, offset)
			if err == nil {
				p.Add(int64(len(chunk)))
			}
			errCh <- err
		}()
	}

	for i := 0; i < chunkCount; i++ {
		if err := <-errCh; err != nil {
			return nil, err
		}
	}

	p.Done()
	info, err := c.Finalize(ctx, sessionID)
	if err == nil && opts.Resume && compress != "zstd" {
		os.Remove(resumeStatePath(originalSHA))
	}
	return info, err
}

// fileSHA256 计算文件的 SHA-256 十六进制字符串。
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// SubmitDir 提交目录 manifest，创建目录记录。
func (c *Client) SubmitDir(ctx context.Context, name string, entries []clientDirEntry) (*FileInfo, error) {
	body, _ := json.Marshal(clientDirManifest{Name: name, Entries: entries})
	u := fmt.Sprintf("%s/v1/dirs?namespace=%s", c.ServerURL, url.QueryEscape(c.Namespace))
	req, _ := http.NewRequestWithContext(ctx, "POST", u, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("submit dir failed (%d): %s", resp.StatusCode, string(body))
	}
	var info FileInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return &info, nil
}

// clientDirManifest 目录 manifest 结构。
type clientDirManifest struct {
	Name    string           `json:"name,omitempty"`
	Entries []clientDirEntry `json:"entries"`
}

// clientDirEntry 目录中单个文件的条目。
type clientDirEntry struct {
	Path   string `json:"path"`
	FileID string `json:"file_id"`
}

// UploadDir 并发上传目录：收集所有文件后以固定并发数上传，最后提交 manifest。
func (c *Client) UploadDir(ctx context.Context, dirPath string, opts UploadOptions) (*FileInfo, error) {
	// 收集文件
	type fileTask struct {
		path string
		rel  string
	}
	var tasks []fileTask
	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dirPath, path)
		if err != nil {
			return err
		}
		tasks = append(tasks, fileTask{path: path, rel: filepath.ToSlash(rel)})
		return nil
	})
	if err != nil {
		return nil, err
	}

	concurrency := opts.Concurrency
	if concurrency <= 0 {
		concurrency = 4
	}
	fmt.Fprintf(os.Stderr, "  上传 %d 个文件（并发 %d）\n", len(tasks), concurrency)

	// 进度：atomic 计数器显示当前正在上传的文件
	var doneCount int32

	type result struct {
		rel   string
		fmeta *FileInfo
		err   error
	}
	sem := make(chan struct{}, concurrency)
	results := make(chan result, len(tasks))

	for _, t := range tasks {
		sem <- struct{}{}
		task := t
		go func() {
			defer func() { <-sem }()
			n := atomic.AddInt32(&doneCount, 1)
			fmt.Fprintf(os.Stderr, "\r  [%d/%d] %s", n, len(tasks), task.rel)
			dirOpts := opts
			dirOpts.FileName = task.rel
			fmeta, err := c.UploadFile(ctx, task.path, dirOpts)
			results <- result{rel: task.rel, fmeta: fmeta, err: err}
		}()
	}

	// drain sem
	go func() {
		for i := 0; i < cap(sem); i++ {
			sem <- struct{}{}
		}
	}()

	var entries []clientDirEntry
	var firstErr error
	for range tasks {
		r := <-results
		if r.err != nil && firstErr == nil {
			firstErr = r.err
		}
		if r.fmeta != nil {
			entries = append(entries, clientDirEntry{Path: r.rel, FileID: r.fmeta.FileID})
		}
	}
	fmt.Fprintln(os.Stderr) // 换行
	if firstErr != nil {
		return nil, firstErr
	}
	dirName := filepath.Base(dirPath)
	return c.SubmitDir(ctx, dirName, entries)
}

// DownloadFile 下载单个文件，返回 HTTP 响应（调用者负责关闭 Body）。
func (c *Client) DownloadFile(ctx context.Context, fileID, rng string) (*http.Response, error) {
	u := fmt.Sprintf("%s/v1/files/%s?namespace=%s", c.ServerURL, url.PathEscape(fileID), url.QueryEscape(c.Namespace))
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	if rng != "" {
		req.Header.Set("Range", "bytes="+rng)
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("download failed (%d): %s", resp.StatusCode, string(body))
	}
	return resp, nil
}

// DownloadDir 下载目录打包文件，返回 HTTP 响应（调用者负责关闭 Body）。
func (c *Client) DownloadDir(ctx context.Context, dirID, format string) (*http.Response, error) {
	u := fmt.Sprintf("%s/v1/dirs/%s?format=%s&namespace=%s", c.ServerURL, url.PathEscape(dirID), url.QueryEscape(format), url.QueryEscape(c.Namespace))
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("download dir failed (%d): %s", resp.StatusCode, string(body))
	}
	return resp, nil
}

func (c *Client) uploadMissingChunks(ctx context.Context, sessionID string, data []byte, chunkSize int64, concurrency int) error {
	status, total, err := c.GetStatus(ctx, sessionID)
	if err != nil {
		return err
	}
	_ = total
	got := make(map[int]bool)
	for _, c := range status {
		got[c.Index] = true
	}

	totalLen := len(data)
	chunkCount := totalLen / int(chunkSize)
	if totalLen%int(chunkSize) != 0 {
		chunkCount++
	}

	sem := make(chan struct{}, concurrency)
	errCh := make(chan error, chunkCount)
	sent := 0

	for i := 0; i < chunkCount; i++ {
		if got[i] {
			continue
		}
		start := i * int(chunkSize)
		end := start + int(chunkSize)
		if end > totalLen {
			end = totalLen
		}
		idx := i
		offset := int64(start)
		chunk := data[start:end]

		sem <- struct{}{}
		sent++
		go func() {
			defer func() { <-sem }()
			errCh <- c.UploadChunk(ctx, sessionID, idx, chunk, offset)
		}()
	}

	for i := 0; i < sent; i++ {
		if err := <-errCh; err != nil {
			return err
		}
	}
	return nil
}

// ChunkStatus 单个分片状态。
type ChunkStatus struct {
	Index  int   `json:"index"`
	Offset int64 `json:"offset"`
	Size   int64 `json:"size"`
}

// GetStatus 查询上传会话的当前状态（已收到的分片列表及总大小）。
func (c *Client) GetStatus(ctx context.Context, sessionID string) ([]ChunkStatus, int64, error) {
	u := fmt.Sprintf("%s/v1/uploads/%s/status?namespace=%s", c.ServerURL, url.PathEscape(sessionID), url.QueryEscape(c.Namespace))
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	resp, err := c.do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, 0, fmt.Errorf("get status failed (%d): %s", resp.StatusCode, string(body))
	}
	var result struct {
		Chunks  []ChunkStatus `json:"chunks"`
		Total   int64         `json:"total"`
		Session string        `json:"session"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, 0, err
	}
	return result.Chunks, result.Total, nil
}

// FileInfo 上传完成后的文件元信息。
type FileInfo struct {
	FileID string `json:"file_id"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Name   string `json:"name"`
}

// CheckExists 秒传预检：查询文件是否已存在。
func (c *Client) CheckExists(ctx context.Context, sha256, name string) (*FileInfo, error) {
	u := fmt.Sprintf("%s/v1/files?sha256=%s&namespace=%s&name=%s", c.ServerURL, url.QueryEscape(sha256), url.QueryEscape(c.Namespace), url.QueryEscape(name))
	req, _ := http.NewRequestWithContext(ctx, "HEAD", u, nil)
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("check exists failed (%d): %s", resp.StatusCode, string(body))
	}
	var info FileInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return &info, nil
}

// CreateSession 创建 tus 上传会话。
func (c *Client) CreateSession(ctx context.Context, size int64, sha256, compression string, chunkSize int64, fileName string) (string, error) {
	u := fmt.Sprintf("%s/uploads", c.ServerURL)
	req, _ := http.NewRequestWithContext(ctx, "POST", u, nil)
	req.Header.Set("Upload-Length", strconv.FormatInt(size, 10))
	req.Header.Set("X-SHA256", sha256)
	req.Header.Set("X-Compression", compression)
	req.Header.Set("X-Chunk-Size", strconv.FormatInt(chunkSize, 10))
	req.Header.Set("X-File-Name", fileName)
	req.Header.Set("X-Namespace", c.Namespace)

	resp, err := c.do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("create session failed (%d): %s", resp.StatusCode, string(body))
	}
	loc := resp.Header.Get("Location")
	loc = strings.TrimPrefix(loc, "/uploads/")
	if loc == "" {
		return "", fmt.Errorf("server returned empty Location")
	}
	return loc, nil
}

// UploadChunk 上传单个分片。
func (c *Client) UploadChunk(ctx context.Context, sessionID string, index int, data []byte, offset int64) error {
	u := fmt.Sprintf("%s/uploads/%s", c.ServerURL, url.PathEscape(sessionID))
	h := sha256Sum(data)
	req, _ := http.NewRequestWithContext(ctx, "PATCH", u, bytes.NewReader(data))
	req.Header.Set("Upload-Offset", strconv.FormatInt(offset, 10))
	req.Header.Set("X-Slice-Index", strconv.Itoa(index))
	req.Header.Set("X-Slice-SHA256", h)
	req.ContentLength = int64(len(data))

	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upload chunk %d failed (%d): %s", index, resp.StatusCode, string(body))
	}
	return nil
}

// Finalize 触发服务端合并文件。
func (c *Client) Finalize(ctx context.Context, sessionID string) (*FileInfo, error) {
	u := fmt.Sprintf("%s/v1/uploads/%s/finalize", c.ServerURL, url.PathEscape(sessionID))
	req, _ := http.NewRequestWithContext(ctx, "POST", u, nil)
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("finalize failed (%d): %s", resp.StatusCode, string(body))
	}
	var info FileInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return &info, nil
}