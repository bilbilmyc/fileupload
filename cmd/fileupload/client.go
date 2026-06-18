package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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
		// 除第一次外，每次重试需要重新构造 body（因为第一次可能已被消费）
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