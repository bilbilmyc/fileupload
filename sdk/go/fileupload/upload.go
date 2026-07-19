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
	"sort"
	"strconv"
	"strings"
	"sync"

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
	if err := validateUploadOptions(opt); err != nil {
		return nil, err
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

	// 计算 SHA-256（用于秒传和校验），然后复位同一文件句柄用于上传。
	originalSHA, err := hashReader(f)
	if err != nil {
		return nil, fmt.Errorf("计算 SHA-256: %w", err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("复位文件读取位置: %w", err)
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
	if err := validateUploadOptions(opt); err != nil {
		return nil, err
	}

	// 从 reader 读取全部数据计算 SHA-256
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("读取数据: %w", err)
	}
	if fileSize < 0 {
		fileSize = int64(len(data))
	} else if fileSize != int64(len(data)) {
		return nil, fmt.Errorf("文件大小不匹配: 指定 %d，实际 %d", fileSize, len(data))
	}
	originalSHA := SHA256Sum(data)

	exists, err := c.CheckExists(ctx, originalSHA, opt.FileName)
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
	var source io.Reader = f
	var sourceCloser io.Closer

	if opt.Compression == "zstd" && fileSize > 0 {
		pr, pw := io.Pipe()
		source = pr
		sourceCloser = pr
		go func() {
			zw, err := zstd.NewWriter(pw)
			if err != nil {
				_ = pw.CloseWithError(err)
				return
			}
			_, copyErr := io.Copy(zw, f)
			closeErr := zw.Close()
			if copyErr == nil {
				copyErr = closeErr
			}
			_ = pw.CloseWithError(copyErr)
		}()
	}
	if sourceCloser != nil {
		defer sourceCloser.Close()
	}

	sem := make(chan struct{}, concurrency)
	errCh := make(chan error, 1)
	uploadCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	reportError := func(err error) {
		if err == nil {
			return
		}
		select {
		case errCh <- err:
			cancel()
		default:
		}
	}

	idx := 0
readLoop:
	for {
		select {
		case <-uploadCtx.Done():
			break readLoop
		default:
		}

		chunk := make([]byte, int(chunkSize))
		n, readErr := io.ReadFull(source, chunk)
		if n > 0 {
			chunk = chunk[:n]
			i := idx
			select {
			case sem <- struct{}{}:
			case <-uploadCtx.Done():
				break readLoop
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				reportError(c.UploadChunk(uploadCtx, sessionID, i, chunk, int64(i)*chunkSize))
			}()
			idx++
		}

		switch readErr {
		case nil:
			continue
		case io.EOF, io.ErrUnexpectedEOF:
			break readLoop
		default:
			reportError(readErr)
			break readLoop
		}
	}

	wg.Wait()
	select {
	case err := <-errCh:
		return nil, err
	default:
	}
	if err := ctx.Err(); err != nil {
		return nil, err
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
	if err := validateUploadOptions(opt); err != nil {
		return nil, err
	}

	type fileTask struct {
		path string
		rel  string
	}
	dirPrefix := filepath.Base(filepath.Clean(dirPath))

	var tasks []fileTask
	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
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
	uploadCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	sem := make(chan struct{}, opt.Concurrency)
	results := make(chan uploadResult, len(tasks))

	started := 0
scheduleLoop:
	for _, t := range tasks {
		select {
		case sem <- struct{}{}:
		case <-uploadCtx.Done():
			break scheduleLoop
		}
		task := t
		started++
		go func() {
			defer func() { <-sem }()
			fmeta, err := c.Upload(uploadCtx, task.path, WithFileName(task.rel), WithCompression(opt.Compression), WithChunkSize(opt.ChunkSize), WithConcurrency(opt.Concurrency))
			results <- uploadResult{rel: task.rel, fmeta: fmeta, err: err}
		}()
	}

	entries := make([]DirEntry, 0, started)
	var firstErr error
	for range started {
		r := <-results
		if r.err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("上传 %s 失败: %w", r.rel, r.err)
				cancel()
			}
			continue
		}
		if r.fmeta != nil {
			entries = append(entries, DirEntry{Path: r.rel, FileID: r.fmeta.FileID})
		}
	}
	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return c.SubmitDir(ctx, dirPrefix, entries)
}

func validateUploadOptions(opt *UploadOptions) error {
	if opt.ChunkSize <= 0 {
		return fmt.Errorf("分片大小必须大于 0")
	}
	if opt.Concurrency <= 0 {
		return fmt.Errorf("上传并发数必须大于 0")
	}
	if opt.Compression != "none" && opt.Compression != "zstd" {
		return fmt.Errorf("不支持的压缩格式 %q", opt.Compression)
	}
	return nil
}

// SubmitDir 提交目录 manifest。
func (c *Client) SubmitDir(ctx context.Context, name string, entries []DirEntry) (*FileInfo, error) {
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
	Name    string     `json:"name,omitempty"`
	Entries []DirEntry `json:"entries"`
}

// DirEntry 描述目录 manifest 中的一个文件。
type DirEntry struct {
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
	if fileID := resp.Header.Get("X-File-ID"); fileID != "" {
		size, err := strconv.ParseInt(resp.Header.Get("X-File-Size"), 10, 64)
		if err != nil || size < 0 {
			return nil, fmt.Errorf("check exists returned invalid X-File-Size %q", resp.Header.Get("X-File-Size"))
		}
		fileSHA := resp.Header.Get("X-File-SHA256")
		if fileSHA == "" {
			fileSHA = sha256
		}
		return &FileInfo{FileID: fileID, SHA256: fileSHA, Size: size, Name: name}, nil
	}

	// 兼容非标准但仍在响应体中返回元数据的旧实现。
	var info FileInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("check exists response missing X-File-ID: %w", err)
	}
	if info.FileID == "" || info.Size < 0 {
		return nil, fmt.Errorf("check exists response contains invalid file metadata")
	}
	if info.Name == "" {
		info.Name = name
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
