package fileupload

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
)

// ===== 文件管理 =====

// Rename 重命名文件或目录（PATCH /v1/files/{id}）
func (c *Client) Rename(ctx context.Context, fileID, newName string) error {
	body, _ := json.Marshal(map[string]string{"name": newName})
	req, err := http.NewRequestWithContext(ctx, "PATCH", c.url("/v1/files/"+fileID), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return &StatusError{Code: resp.StatusCode, Body: readBody(resp)}
	}
	return nil
}

// ===== 分享 =====

// ShareEntry 分享链接记录
type ShareEntry struct {
	Token         string `json:"token"`
	FileID        string `json:"file_id"`
	PasswordHash  string `json:"-"`
	ExpiresAt     string `json:"expires_at,omitempty"`
	MaxDownloads  int    `json:"max_downloads"`
	CurDownloads  int    `json:"cur_downloads"`
	Namespace     string `json:"namespace"`
}

// CreateShareRequest 创建分享请求体
type CreateShareRequest struct {
	FileID       string `json:"file_id"`
	Password     string `json:"password,omitempty"`
	ExpiresIn    int    `json:"expires_in"`     // 过期小时数，0=不限
	MaxDownloads int    `json:"max_downloads"`  // 0=不限
}

// CreateShare 创建分享链接（POST /v1/share）
func (c *Client) CreateShare(ctx context.Context, req CreateShareRequest) (*ShareEntry, error) {
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.url("/v1/share"), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, &StatusError{Code: resp.StatusCode, Body: readBody(resp)}
	}
	var entry ShareEntry
	if err := json.NewDecoder(resp.Body).Decode(&entry); err != nil {
		return nil, err
	}
	return &entry, nil
}

// AccessShare 通过 token 访问分享链接（GET /s/{token}）
func (c *Client) AccessShare(ctx context.Context, token string) (*ShareEntry, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.url("/s/"+token), nil)
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
	var entry ShareEntry
	if err := json.NewDecoder(resp.Body).Decode(&entry); err != nil {
		return nil, err
	}
	return &entry, nil
}

// ShareURL 构造分享访问 URL
func (c *Client) ShareURL(token string) string {
	return c.url("/s/" + token)
}

// ===== 后台管理 =====

// SystemStatus 系统状态（GET /v1/admin/status）
type SystemStatus struct {
	Version   string         `json:"version"`
	StartTime string         `json:"start_time"`
	Uptime    string         `json:"uptime"`
	Storage   map[string]any `json:"storage"`
	Metadata  map[string]any `json:"metadata"`
	Counts    map[string]int `json:"counts"`
}

// GetSystemStatus 获取系统状态
func (c *Client) GetSystemStatus(ctx context.Context) (*SystemStatus, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.url("/v1/admin/status"), nil)
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
	var status SystemStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, err
	}
	return &status, nil
}

// AuditLogEntry 审计日志条目
type AuditLogEntry struct {
	ID         string `json:"id"`
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
	Namespace  string `json:"namespace"`
	Detail     string `json:"detail"`
	CreatedAt  string `json:"created_at"`
}

// ListAuditLogs 查询审计日志（GET /v1/admin/audit）
func (c *Client) ListAuditLogs(ctx context.Context, page, perPage int) ([]AuditLogEntry, int, error) {
	q := "?page=" + strconv.Itoa(page) + "&per_page=" + strconv.Itoa(perPage)
	req, err := http.NewRequestWithContext(ctx, "GET", c.url("/v1/admin/audit"+q), nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, &StatusError{Code: resp.StatusCode, Body: readBody(resp)}
	}
	var out struct {
		Entries []AuditLogEntry `json:"entries"`
		Total   int              `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, 0, err
	}
	return out.Entries, out.Total, nil
}

// ScanReport 一致性巡检报告（POST /v1/admin/scan）
type ScanReport struct {
	OrphanParts     int      `json:"orphan_parts"`
	OrphanFiles     []string `json:"orphan_files"`
	MetadataOrphans int      `json:"metadata_orphans"`
	RefCountFixes   int      `json:"ref_count_fixes"`
	CorruptedFiles  []string `json:"corrupted_files"`
}

// TriggerScan 触发一致性巡检
func (c *Client) TriggerScan(ctx context.Context) (*ScanReport, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", c.url("/v1/admin/scan"), nil)
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
	var report ScanReport
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		return nil, err
	}
	return &report, nil
}