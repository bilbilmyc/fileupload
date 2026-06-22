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

	"github.com/klauspost/compress/zstd"
)

const defaultNamespace = "default"

// Client 统一的文件上传下载 HTTP 客户端。
type Client struct {
	ServerURL  string
	Namespace  string
	Token      string
	TokenHeader string
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
		ServerURL:   strings.TrimRight(serverURL, "/"),
		Namespace:   namespace,
		TokenHeader: "X-Auth-Token",
		HTTPClient:  &http.Client{Timeout: 60 * time.Second},
	}
}

// SetToken 设置认证令牌，后续所有请求将携带 X-Auth-Token
func (c *Client) SetToken(token string) {
	c.Token = token
}

// do 执行 HTTP 请求，自动注入认证令牌，对 5xx/503 自动重试最多 3 次。
func (c *Client) do(req *http.Request) (*http.Response, error) {
	// 注入认证令牌
	if c.Token != "" {
		req.Header.Set(c.TokenHeader, c.Token)
	}
	if c.Namespace != "" && req.Header.Get("X-Namespace") == "" {
		req.Header.Set("X-Namespace", c.Namespace)
	}

	var lastErr error
	for attempt := range 3 {
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
	ChunkSize     int64
	Concurrency   int
	Compress      string
	Resume        bool
	FileName      string
	NoProgress    bool
	ExcludeHidden bool
}

// UploadFile 上传单个文件。支持压缩、并发分片、秒传、断点续传。
// 实现为流式：不将整个文件读入内存。
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
	fileName := opts.FileName
	if fileName == "" {
		fileName = info.Name()
	}
	fileSize := info.Size()

	originalSHA, err := fileSHA256(localPath)
	if err != nil {
		return nil, err
	}

	exists, err := c.CheckExists(ctx, originalSHA, fileName)
	if err != nil {
		return nil, err
	}
	if exists != nil {
		return exists, nil
	}

	chunkSize := opts.ChunkSize
	if chunkSize <= 0 {
		chunkSize = 10 * 1024 * 1024
	}
	concurrency := opts.Concurrency
	if concurrency <= 0 {
		concurrency = 4
	}

	compress := opts.Compress
	if compress == "" {
		compress = "none"
	}

	sessionID, err := c.CreateSession(ctx, fileSize, originalSHA, compress, chunkSize, fileName)
	if err != nil {
		return nil, err
	}

	uploaded := make(map[int]bool)
	if opts.Resume {
		status, _, _ := c.GetStatus(ctx, sessionID)
		for _, ch := range status {
			uploaded[ch.Index] = true
		}
	}

	var p *Progress
	if !opts.NoProgress && fileSize > 0 {
		p = NewProgress(fileSize, "Uploading")
		p.Start()
		defer p.Stop()
	}

	saveResume := opts.Resume && compress != "zstd"
	if saveResume {
		saveResumeState(resumeStatePath(originalSHA), resumeState{
			SessionID: sessionID,
			SHA256:    originalSHA,
			FileSize:  fileSize,
		})
	}

	var source io.Reader
	var byteCounter *countingReader
	if compress == "zstd" && fileSize > 0 {
		byteCounter = &countingReader{r: f}
		pr, pw := io.Pipe()
		go func() {
			zw, err := zstd.NewWriter(pw)
			if err != nil {
				pw.CloseWithError(err)
				return
			}
			_, err = io.Copy(zw, byteCounter)
			zw.Close()
			pw.CloseWithError(err)
		}()
		source = pr
	} else {
		byteCounter = &countingReader{r: f}
		source = byteCounter
	}

	if err := c.uploadStream(ctx, sessionID, source, chunkSize, concurrency, p, uploaded, byteCounter); err != nil {
		return nil, err
	}

	if p != nil {
		p.Done()
	}
	fi, err := c.Finalize(ctx, sessionID)
	if err == nil && saveResume {
		os.Remove(resumeStatePath(originalSHA))
	}
	return fi, err
}

// countingReader 统计已读取字节的 reader。
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func (c *countingReader) N() int64 {
	return c.n
}

// uploadStream 从 reader 流式读取分片并上传。
func (c *Client) uploadStream(
	ctx context.Context,
	sessionID string,
	source io.Reader,
	chunkSize int64,
	concurrency int,
	p *Progress,
	uploaded map[int]bool,
	counter *countingReader,
) error {
	buf := make([]byte, chunkSize)
	idx := 0
	offset := int64(0)
	sem := make(chan struct{}, concurrency)
	errCh := make(chan error, concurrency)
	sent := 0
	var doneErr error
	var lastCounter int64

	for {
		n, err := io.ReadFull(source, buf)
		if n == 0 {
			break
		}
		chunk := buf[:n]
		i := idx
		o := offset

		updateProgress := func() {
			if p == nil {
				return
			}
			now := counter.N()
			delta := now - lastCounter
			if delta > 0 {
				lastCounter = now
				p.Add(delta)
			}
		}

		if !uploaded[i] {
			sem <- struct{}{}
			sent++
			go func() {
				defer func() { <-sem }()
				err := c.UploadChunk(ctx, sessionID, i, chunk, o)
				if err == nil {
					updateProgress()
				}
				errCh <- err
			}()
		} else {
			updateProgress()
		}

		idx++
		offset += int64(n)
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			doneErr = err
			break
		}
	}

	for i := 0; i < sent; i++ {
		if err := <-errCh; err != nil {
			return err
		}
	}
	return doneErr
}

// uploadBytes 将内存中的字节上传到服务端（用于 bench 和旧路径）。
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

	var p *Progress
	if !opts.NoProgress && total > 0 {
		p = NewProgress(int64(total), "Uploading")
		p.Start()
		defer p.Stop()
	}

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
			if err == nil && p != nil {
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

	if p != nil {
		p.Done()
	}
	return c.Finalize(ctx, sessionID)
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
	type fileTask struct {
		path string
		rel  string
		mrel string
	}
	dirPrefix := filepath.Base(dirPath)

	var tasks []fileTask
	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if opts.ExcludeHidden {
			if len(info.Name()) > 0 && info.Name()[0] == '.' {
				return nil
			}
		}
		rel, err := filepath.Rel(dirPath, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		tasks = append(tasks, fileTask{
			path: path,
			rel:  dirPrefix + "/" + rel,
			mrel: rel,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return nil, fmt.Errorf("目录中没有可上传的文件（所有文件为空或隐藏）")
	}

	concurrency := opts.Concurrency
	if concurrency <= 0 {
		concurrency = 4
	}
	opts.NoProgress = true
	fmt.Fprintf(os.Stderr, "  上传 %d 个文件（并发 %d）\n", len(tasks), concurrency)

	var doneCount int32
	type result struct {
		rel   string
		mrel  string
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
			results <- result{rel: task.rel, mrel: task.mrel, fmeta: fmeta, err: err}
		}()
	}

	go func() {
		for i := 0; i < cap(sem); i++ {
			sem <- struct{}{}
		}
	}()

	var entries []clientDirEntry
	var errs []string
	for range tasks {
		r := <-results
		if r.err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", r.rel, r.err))
		}
		if r.fmeta != nil {
			entries = append(entries, clientDirEntry{Path: r.mrel, FileID: r.fmeta.FileID})
		}
	}
	fmt.Fprintln(os.Stderr)
	if len(errs) > 0 {
		fmt.Fprintf(os.Stderr, "  失败 %d 个文件:\n", len(errs))
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "    - %s\n", e)
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("没有文件上传成功")
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
