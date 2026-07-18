package fileupload

import (
	"bytes"
	"context"
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

	"github.com/klauspost/compress/zstd"
)

// Upload 上传本地文件到服务端。支持压缩、并发分片、秒传。
//
// 示例:
//
//	info, err := client.Upload(ctx, "large-file.dat",
//	    fileupload.WithConcurrency(8),
//	    fileupload.WithCompression("zstd"),
//	)
func (c *Client) Upload(ctx context.Context, localPath string, opts ...UploadOption) (*FileInfo, error) {
	opt := &UploadOptions{
		ChunkSize:   DefaultChunkSize,
		Concurrency: DefaultConcurrency,
		Compression: "none",
		FileName:    filepath.Base(localPath),
	}
	for _, o := range opts {
		o(opt)
	}

	f, err := os.Open(localPath)
	if err != nil {
		return nil, fmt.Errorf("打开文件: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("获取文件信息: %w", err)
	}
	fileSize := info.Size()

	// 计算 SHA-256（用于秒传和校验）
	originalSHA, err := FileSHA256(localPath)
	if err != nil {
		return nil, fmt.Errorf("计算 SHA-256: %w", err)
	}

	// 秒传预检
	exists, err := c.CheckExists(ctx, originalSHA, opt.FileName)
	if err != nil {
		return nil, err
	}
	if exists != nil {
		return exists, nil
	}

	return c.uploadStream(ctx, f, fileSize, originalSHA, opt)
}

// UploadReader 从 io.Reader 上传数据流。
// 需要指定 fileName 和 fileSize（-1 表示未知，将禁用进度估算）。
func (c *Client) UploadReader(ctx context.Context, r io.Reader, fileSize int64, fileName string, opts ...UploadOption) (*FileInfo, error) {
	opt := &UploadOptions{
		ChunkSize:   DefaultChunkSize,
		Concurrency: DefaultConcurrency,
		Compression: "none",
		FileName:    fileName,
	}
	for _, o := range opts {
		o(opt)
	}

	// 从 reader 读取全部数据计算 SHA-256
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("读取数据: %w", err)
	}
	if fileSize <= 0 {
		fileSize = int64(len(data))
	}
	originalSHA := SHA256Sum(data)

	exists, err := c.CheckExists(ctx, originalSHA, fileName)
	if err != nil {
		return nil, err
	}
	if exists != nil {
		return exists, nil
	}

	return c.uploadBytes(ctx, data, fileSize, originalSHA, opt)
}

// uploadStream 从 reader 流式读取并分片上传。
func (c *Client) uploadStream(ctx context.Context, f *os.File, fileSize int64, sha256 string, opt *UploadOptions) (*FileInfo, error) {
	sessionID, err := c.CreateSession(ctx, fileSize, sha256, opt.Compression, opt.ChunkSize, opt.FileName)
	if err != nil {
		return nil, err
	}

	chunkSize := opt.ChunkSize
	concurrency := opt.Concurrency

	var source io.Reader

	if opt.Compression == "zstd" && fileSize > 0 {
		pr, pw := io.Pipe()
		go func() {
			zw, err := zstd.NewWriter(pw)
			if err != nil {
				pw.CloseWithError(err)
				return
			}
			_, err = io.Copy(zw, f)
			zw.Close()
			pw.CloseWithError(err)
		}()
		source = pr
	} else {
		source = f
	}

	buf := make([]byte, chunkSize)
	idx := 0
	sem := make(chan struct{}, concurrency)
	errCh := make(chan error, concurrency)
	sent := 0

	for {
		n, err := io.ReadFull(source, buf)
		if n == 0 {
			break
		}
		chunk := buf[:n]
		i := idx

		sem <- struct{}{}
		sent++
		go func() {
			defer func() { <-sem }()
			errCh <- c.UploadChunk(ctx, sessionID, i, chunk, int64(i)*chunkSize)
		}()

		idx++
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}

	for i := 0; i < sent; i++ {
		if err := <-errCh; err != nil {
			return nil, err
		}
	}

	return c.Finalize(ctx, sessionID)
}

// uploadBytes 从内存数据上传。
func (c *Client) uploadBytes(ctx context.Context, data []byte, fileSize int64, sha256 string, opt *UploadOptions) (*FileInfo, error) {
	sessionID, err := c.CreateSession(ctx, fileSize, sha256, opt.Compression, opt.ChunkSize, opt.FileName)
	if err != nil {
		return nil, err
	}

	total := len(data)
	chunkSize := int(opt.ChunkSize)
	concurrency := opt.Concurrency
	chunkCount := total / chunkSize
	if total%chunkSize != 0 {
		chunkCount++
	}

	sem := make(chan struct{}, concurrency)
	errCh := make(chan error, chunkCount)

	for i := 0; i < chunkCount; i++ {
		start := i * chunkSize
		end := start + chunkSize
		if end > total {
			end = total
		}
		idx := i

		sem <- struct{}{}
		go func() {
			defer func() { <-sem }()
			errCh <- c.UploadChunk(ctx, sessionID, idx, data[start:end], int64(start))
		}()
	}

	for i := 0; i < chunkCount; i++ {
		if err := <-errCh; err != nil {
			return nil, err
		}
	}

	return c.Finalize(ctx, sessionID)
}

// UploadDir 上传目录。
func (c *Client) UploadDir(ctx context.Context, dirPath string, opts ...UploadOption) (*FileInfo, error) {
	opt := &UploadOptions{
		ChunkSize:   DefaultChunkSize,
		Concurrency: DefaultConcurrency,
		Compression: "none",
	}
	for _, o := range opts {
		o(opt)
	}

	type fileTask struct {
		path string
		rel  string
	}
	dirPrefix := filepath.Base(dirPath)

	var tasks []fileTask
	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dirPath, path)
		if err != nil {
			return err
		}
		tasks = append(tasks, fileTask{path: path, rel: dirPrefix + "/" + filepath.ToSlash(rel)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return nil, fmt.Errorf("目录中没有文件")
	}

	type uploadResult struct {
		rel   string
		fmeta *FileInfo
		err   error
	}
	sem := make(chan struct{}, opt.Concurrency)
	results := make(chan uploadResult, len(tasks))

	var doneCount int32
	for _, t := range tasks {
		sem <- struct{}{}
		task := t
		go func() {
			defer func() { <-sem }()
			atomic.AddInt32(&doneCount, 1)
			fileOpts := *opt
			fileOpts.FileName = task.rel
			fmeta, err := c.Upload(ctx, task.path, WithFileName(task.rel), WithCompression(opt.Compression), WithChunkSize(opt.ChunkSize), WithConcurrency(opt.Concurrency))
			results <- uploadResult{rel: task.rel, fmeta: fmeta, err: err}
		}()
	}

	var entries []clientDirEntry
	for range tasks {
		r := <-results
		if r.err != nil {
			return nil, fmt.Errorf("上传 %s 失败: %w", r.rel, r.err)
		}
		if r.fmeta != nil {
			entries = append(entries, clientDirEntry{Path: r.rel, FileID: r.fmeta.FileID})
		}
	}

	return c.SubmitDir(ctx, dirPrefix, entries)
}

// SubmitDir 提交目录 manifest。
func (c *Client) SubmitDir(ctx context.Context, name string, entries []clientDirEntry) (*FileInfo, error) {
	body, _ := json.Marshal(dirManifest{Name: name, Entries: entries})
	req, err := http.NewRequestWithContext(ctx, "POST", c.url("/v1/dirs"), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("submit dir failed (%d): %s", resp.StatusCode, readBody(resp))
	}
	var info FileInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return &info, nil
}

type dirManifest struct {
	Name    string           `json:"name,omitempty"`
	Entries []clientDirEntry `json:"entries"`
}

type clientDirEntry struct {
	Path   string `json:"path"`
	FileID string `json:"file_id"`
}

// ---- Session API ----

// CreateSession 创建上传会话。
func (c *Client) CreateSession(ctx context.Context, size int64, sha256, compression string, chunkSize int64, fileName string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", c.url("/uploads"), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Upload-Length", strconv.FormatInt(size, 10))
	req.Header.Set("X-SHA256", sha256)
	req.Header.Set("X-Compression", compression)
	req.Header.Set("X-Chunk-Size", strconv.FormatInt(chunkSize, 10))
	req.Header.Set("X-File-Name", fileName)

	resp, err := c.do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("create session failed (%d): %s", resp.StatusCode, readBody(resp))
	}
	loc := strings.TrimPrefix(resp.Header.Get("Location"), "/uploads/")
	if loc == "" {
		return "", fmt.Errorf("empty Location header")
	}
	return loc, nil
}

// UploadChunk 上传单个分片。
func (c *Client) UploadChunk(ctx context.Context, sessionID string, index int, data []byte, offset int64) error {
	h := SHA256Sum(data)
	req, err := http.NewRequestWithContext(ctx, "PATCH", c.url("/uploads/"+url.PathEscape(sessionID)), bytes.NewReader(data))
	if err != nil {
		return err
	}
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
		return fmt.Errorf("upload chunk %d failed (%d): %s", index, resp.StatusCode, readBody(resp))
	}
	return nil
}

// GetStatus 查询上传会话状态。
func (c *Client) GetStatus(ctx context.Context, sessionID string) ([]ChunkStatus, int64, error) {
	resp, err := c.do(getRequest(ctx, "GET", c.url("/v1/uploads/"+url.PathEscape(sessionID)+"/status")))
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("get status failed (%d): %s", resp.StatusCode, readBody(resp))
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

// CheckExists 秒传预检。
func (c *Client) CheckExists(ctx context.Context, sha256, name string) (*FileInfo, error) {
	u := c.url("/v1/files?sha256=" + url.QueryEscape(sha256) + "&name=" + url.QueryEscape(name))
	req, err := http.NewRequestWithContext(ctx, "HEAD", u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("check exists failed (%d): %s", resp.StatusCode, readBody(resp))
	}
	var info FileInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return &info, nil
}

// Finalize 完成上传。
func (c *Client) Finalize(ctx context.Context, sessionID string) (*FileInfo, error) {
	resp, err := c.do(postRequest(ctx, "POST", c.url("/v1/uploads/"+url.PathEscape(sessionID)+"/finalize")))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("finalize failed (%d): %s", resp.StatusCode, readBody(resp))
	}
	var info FileInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return &info, nil
}
