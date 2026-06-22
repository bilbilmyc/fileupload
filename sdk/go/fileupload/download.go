package fileupload

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Download 下载文件，返回 HTTP 响应体（调用者需关闭 Body）。
//
// 示例:
//
//	resp, err := client.Download(ctx, "file-id")
//	if err != nil { ... }
//	defer resp.Body.Close()
//	io.Copy(outputFile, resp.Body)
func (c *Client) Download(ctx context.Context, fileID, rangeStr string) (*http.Response, error) {
	u := c.url("/v1/files/" + url.PathEscape(fileID))
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	if rangeStr != "" {
		req.Header.Set("Range", "bytes="+rangeStr)
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		body := readBody(resp)
		return nil, fmt.Errorf("download failed (%d): %s", resp.StatusCode, body)
	}
	return resp, nil
}

// DownloadDir 下载目录打包文件，返回 HTTP 响应体（调用者需关闭 Body）。
//
// format 支持: "zip", "tar.gz", "tar.zst"
func (c *Client) DownloadDir(ctx context.Context, dirID, format string) (*http.Response, error) {
	if format == "" {
		format = "tar.gz"
	}
	u := c.url("/v1/dirs/"+url.PathEscape(dirID), "format="+url.QueryEscape(format))
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		body := readBody(resp)
		return nil, fmt.Errorf("download dir failed (%d): %s", resp.StatusCode, body)
	}
	return resp, nil
}

// BatchDownload 批量下载打包文件，返回 HTTP 响应体。
func (c *Client) BatchDownload(ctx context.Context, ids []string, format string) (*http.Response, error) {
	if format == "" {
		format = "zip"
	}
	idStr := strings.Join(ids, ",")
	u := c.url("/v1/batch/download", "ids="+url.QueryEscape(idStr)+"&format="+url.QueryEscape(format))
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		body := readBody(resp)
		return nil, fmt.Errorf("batch download failed (%d): %s", resp.StatusCode, body)
	}
	return resp, nil
}

// ---- 管理操作 ----

// List 列出目录内容。
func (c *Client) List(ctx context.Context, parentID string) (*ListResult, error) {
	u := c.url("/v1/ls", "parent="+url.QueryEscape(parentID))
	resp, err := c.do(getRequest(ctx, "GET", u))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list failed (%d): %s", resp.StatusCode, readBody(resp))
	}
	var res ListResult
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	return &res, nil
}

// Stat 查询文件或目录信息。
func (c *Client) Stat(ctx context.Context, id string) (*StatResult, error) {
	u := c.url("/v1/stat/" + url.PathEscape(id))
	resp, err := c.do(getRequest(ctx, "GET", u))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("stat failed (%d): %s", resp.StatusCode, readBody(resp))
	}
	var res StatResult
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	return &res, nil
}

// Delete 删除文件。
func (c *Client) Delete(ctx context.Context, id string) error {
	u := c.url("/v1/files/" + url.PathEscape(id))
	resp, err := c.do(getRequest(ctx, "DELETE", u))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("delete failed (%d): %s", resp.StatusCode, readBody(resp))
	}
	return nil
}

// DeleteDir 删除目录。
func (c *Client) DeleteDir(ctx context.Context, id string) error {
	u := c.url("/v1/dirs/" + url.PathEscape(id))
	resp, err := c.do(getRequest(ctx, "DELETE", u))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("delete dir failed (%d): %s", resp.StatusCode, readBody(resp))
	}
	return nil
}

// BatchDelete 批量删除。
func (c *Client) BatchDelete(ctx context.Context, ids []string) (map[string]any, error) {
	body, _ := json.Marshal(map[string][]string{"ids": ids})
	req, err := http.NewRequestWithContext(ctx, "POST", c.url("/v1/batch/delete"), strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("batch delete failed (%d): %s", resp.StatusCode, readBody(resp))
	}
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	return result, nil
}

// Scan 触发服务端一致性巡检。
func (c *Client) Scan(ctx context.Context) (map[string]any, error) {
	resp, err := c.do(postRequest(ctx, "POST", c.url("/v1/admin/scan")))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("scan failed (%d): %s", resp.StatusCode, readBody(resp))
	}
	var res map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	return res, nil
}
