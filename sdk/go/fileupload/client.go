// Package fileupload 提供文件上传下载服务的 Go SDK。
//
// 基本用法:
//
//	client := fileupload.NewClient("http://localhost:8080")
//	client.SetToken("your-jwt-token")
//
//	// 上传文件
//	info, err := client.Upload(ctx, "photo.jpg")
//
//	// 下载文件
//	resp, err := client.Download(ctx, "file-id")
//	defer resp.Body.Close()
//	io.Copy(outputFile, resp.Body)
//
//	// 列目录
//	result, err := client.List(ctx, "/")
//
//	// 删除
//	err := client.Delete(ctx, "file-id")
//
// SDK 支持选项模式:
//
//	client.Upload(ctx, "large-file.dat",
//	    fileupload.WithConcurrency(8),
//	    fileupload.WithCompression("zstd"),
//	    fileupload.WithChunkSize(20*1024*1024),
//	)
package fileupload

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Defaults
const (
	DefaultServerURL   = "http://localhost:8080"
	DefaultNamespace   = "default"
	DefaultChunkSize   = 10 * 1024 * 1024 // 10MB
	DefaultConcurrency = 4
	DefaultTimeout     = 60 * time.Second
	MaxRetries         = 3
)

// Client 文件上传下载服务的 HTTP 客户端。
// 所有方法都接受 context.Context 用于超时和取消。
type Client struct {
	serverURL   string
	namespace   string
	token       string
	tokenHeader string
	httpClient  *http.Client
}

// NewClient 创建客户端实例。
// serverURL: 服务端地址，为空则使用 http://localhost:8080
// namespace: 命名空间，为空则使用 "default"
func NewClient(serverURL, namespace string) *Client {
	if serverURL == "" {
		serverURL = DefaultServerURL
	}
	if namespace == "" {
		namespace = DefaultNamespace
	}
	return &Client{
		serverURL:   strings.TrimRight(serverURL, "/"),
		namespace:   namespace,
		tokenHeader: "Authorization",
		httpClient:  &http.Client{Timeout: DefaultTimeout},
	}
}

// SetToken 设置 JWT 认证令牌，后续所有请求将携带 Authorization: Bearer 头。
func (c *Client) SetToken(token string) {
	c.token = token
}

// SetTokenHeader 设置自定义认证头（默认 Authorization）。
func (c *Client) SetTokenHeader(header string) {
	c.tokenHeader = header
}

// SetNamespace 设置命名空间。
func (c *Client) SetNamespace(ns string) {
	c.namespace = ns
}

// SetHTTPClient 设置自定义 HTTP 客户端。
func (c *Client) SetHTTPClient(client *http.Client) {
	c.httpClient = client
}

// ---- HTTP Helpers ----

// do 执行 HTTP 请求，自动注入认证头和命名空间，对可重试错误最多重试 3 次。
func (c *Client) do(req *http.Request) (*http.Response, error) {
	if c.token != "" {
		if strings.HasPrefix(c.tokenHeader, "Authorization") {
			req.Header.Set("Authorization", "Bearer "+c.token)
		} else {
			req.Header.Set(c.tokenHeader, c.token)
		}
	}
	if req.Header.Get("X-Namespace") == "" {
		req.Header.Set("X-Namespace", c.namespace)
	}

	var lastErr error
	for attempt := range MaxRetries {
		if attempt > 0 && req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			req.Body = body
		}
		resp, err := c.httpClient.Do(req)
		if err == nil {
			if resp.StatusCode != http.StatusServiceUnavailable && resp.StatusCode < 500 {
				return resp, nil
			}
			if attempt == MaxRetries-1 {
				return resp, nil
			}
			resp.Body.Close()
		} else {
			lastErr = err
			if attempt == MaxRetries-1 {
				break
			}
		}

		delay := time.Duration(attempt+1) * 500 * time.Millisecond
		if err := sleepWithContext(req.Context(), delay); err != nil {
			return nil, err
		}
	}
	return nil, lastErr
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// url 生成带命名空间的完整 URL。
func (c *Client) url(path string, query ...string) string {
	u := c.serverURL + path
	if len(query) > 0 && query[0] != "" {
		u += "?" + query[0]
	}
	if !strings.Contains(u, "namespace=") {
		sep := "?"
		if strings.Contains(u, "?") {
			sep = "&"
		}
		u += sep + "namespace=" + url.QueryEscape(c.namespace)
	}
	return u
}

// readBody 读取响应体并关闭，用于错误响应。
func readBody(resp *http.Response) string {
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return string(body)
}

// ---- Types ----

// FileInfo 文件元信息。
type FileInfo struct {
	FileID string `json:"file_id"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Name   string `json:"name"`
}

// ListResult 目录列表结果。
type ListResult struct {
	Dir      any              `json:"dir"`
	Children []map[string]any `json:"children"`
}

// StatResult 文件/目录信息查询结果。
type StatResult struct {
	File map[string]any `json:"file"`
	Blob map[string]any `json:"blob"`
}

// ChunkStatus 单个分片状态。
type ChunkStatus struct {
	Index  int   `json:"index"`
	Offset int64 `json:"offset"`
	Size   int64 `json:"size"`
}

// UploadOptions 上传选项。
type UploadOptions struct {
	ChunkSize   int64
	Concurrency int
	Compression string // "none" 或 "zstd"
	FileName    string
}

// UploadOption 上传选项函数。
type UploadOption func(*UploadOptions)

// WithChunkSize 设置分片大小。
func WithChunkSize(size int64) UploadOption {
	return func(o *UploadOptions) { o.ChunkSize = size }
}

// WithConcurrency 设置上传并发数。
func WithConcurrency(n int) UploadOption {
	return func(o *UploadOptions) { o.Concurrency = n }
}

// WithCompression 启用 zstd 压缩上传。
func WithCompression(f string) UploadOption {
	return func(o *UploadOptions) { o.Compression = f }
}

// WithFileName 设置服务端存储的文件名。
func WithFileName(name string) UploadOption {
	return func(o *UploadOptions) { o.FileName = name }
}

// ---- Helper Functions ----

// SHA256Sum 计算数据的 SHA-256 十六进制字符串。
func SHA256Sum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// FileSHA256 计算文件的 SHA-256 十六进制字符串。
func FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return hashReader(f)
}

func hashReader(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ---- 请求构造辅助 ----

// getRequest 创建 GET 请求。
func getRequest(ctx context.Context, method, url string) *http.Request {
	req, _ := http.NewRequestWithContext(ctx, method, url, nil)
	return req
}

// postRequest 创建 POST 请求。
func postRequest(ctx context.Context, method, url string) *http.Request {
	req, _ := http.NewRequestWithContext(ctx, method, url, nil)
	return req
}
