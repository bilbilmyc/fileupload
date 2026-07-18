package fileupload

import (
	"context"
	"encoding/json"
	"net/http"
)

// SDKChunkInfo 上传分片信息（避免依赖 domain 包）
type SDKChunkInfo struct {
	Index  int    `json:"index"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// UploadStatusResult 上传分片状态查询结果
type UploadStatusResult struct {
	SessionID string         `json:"session_id"`
	Chunks    []SDKChunkInfo `json:"chunks"`
	Total     int            `json:"total"`
}

// GetUploadStatus 查询上传分片状态（GET /v1/uploads/{id}/status）
func (c *Client) GetUploadStatus(ctx context.Context, sessionID string) (*UploadStatusResult, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.url("/v1/uploads/"+sessionID+"/status"), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &StatusError{Code: resp.StatusCode, Body: readBody(resp)}
	}
	var out UploadStatusResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CancelUpload 取消 tus 上传（DELETE /uploads/{id}）
func (c *Client) CancelUpload(ctx context.Context, sessionID string) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", c.url("/uploads/"+sessionID), nil)
	if err != nil {
		return err
	}
	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return &StatusError{Code: resp.StatusCode, Body: readBody(resp)}
	}
	return nil
}

// PreviewURL 返回文件预览 URL（GET /v1/preview/{id}）
// 调用方自行处理 HTTP 请求与 Blob 读取。
func (c *Client) PreviewURL(id string) string {
	return c.url("/v1/preview/"+id, "namespace="+c.namespace)
}

// Preview 下载文件预览（GET /v1/preview/{id}），返回原始 HTTP 响应
// （调用方负责关闭 Body 并按需解码 Blob/字节流）。
func (c *Client) Preview(ctx context.Context, id string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.url("/v1/preview/"+id), nil)
	if err != nil {
		return nil, err
	}
	return c.do(req)
}
