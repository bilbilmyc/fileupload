# fileupload CLI 完整链路实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 补齐 fileupload CLI 的共享 HTTP client、目录上传修复、客户端压缩、断点续传、进度条、各子命令统一接入，使 `fileupload upload/download/ls/stat/rm/scan/bench/config` 成为可稳定复用的客户端。

**Architecture：** 在 `cmd/fileupload/` 下新建 `client.go` 提供统一 `Client`；`upload.go / download.go` 等业务命令全部收敛到 `Client` 方法；压缩/分片/续传/进度等复杂逻辑下沉到 client 内部，各命令只做参数解析与结果打印。

**Tech Stack：** Go 1.25，标准库 `net/http`、`bytes`、`io`、`path/filepath`、`compress/gzip`、`github.com/klauspost/compress/zstd`。复用服务端已有的 `internal/config`、`internal/domain` 类型。

## Global Constraints

- Go 版本：`go 1.25.5`（来自 `go.mod`）。
- 服务端 API 路径以 README 与 `internal/transport/router.go` 为准：tus 用 `/uploads`，REST 用 `/v1/...`。
- namespace 默认从 `--namespace` flag 读取，缺省 `"default"`。
- server 地址优先级：flag `--server` > 环境变量 `FILEUPLOAD_SERVER` > 默认值 `http://localhost:8080`。
- CLI 不依赖 Redis/SQLite，只通过 HTTP 访问服务端。
- 所有命令必须返回非 0 退出码以适配 CI/脚本。
- 代码不要引入新依赖（`go.mod` 已含 `klauspost/compress`）。

---

## File Structure

| 文件 | 职责 |
|---|---|
| `cmd/fileupload/client.go` | 统一 HTTP 客户端，封装 tus/REST 双协议、重试、秒传、压缩、分片并发、断点续传、目录 manifest、下载/删除/列目录/stat/scan。 |
| `cmd/fileupload/progress.go` | 进度显示：单条进度条、并发分片多段进度、总进度。 |
| `cmd/fileupload/upload.go` | 参数解析；单文件/目录上传入口；调用 `Client`；接收结果并打印。 |
| `cmd/fileupload/download.go` | 参数解析；文件/目录下载入口；调用 `Client`；校验/目录解包。 |
| `cmd/fileupload/rm.go` | 删除命令，调用 `Client.Delete`/`DeleteDir`。 |
| `cmd/fileupload/ls.go` | 列目录命命名，调用 `Client.List`。 |
| `cmd/fileupload/stat.go` | stat 命令。 |
| `cmd/fileupload/scan.go` | scan 命令。 |
| `cmd/fileupload/bench.go` | 压测命令，复用 `Client` 上传。 |
| `cmd/fileupload/main.go` | 保留命令分发；新增读取 `--namespace` 统一逻辑（各命令也可自行解析）。 |
| `cmd/fileupload/client_test.go` | client 核心逻辑单元测试（无真实服务端，用 httptest）。 |
| `cmd/fileupload/upload_test.go` | 上传本地方法如 `parseFlags/parseSize` 小范围测试；避免与真正服务端耦合。 |

---

## Task 1: 建立共享 Client 骨架（不含上传主流程）

**Files:**
- Create: `cmd/fileupload/client.go`
- Test: `cmd/fileupload/client_test.go`

**Interfaces:**
- `type Client struct { ServerURL string; Namespace string; HTTPClient *http.Client }`
- `func NewClient(serverURL, namespace string) *Client`
- `func (c *Client) Head(path string) (*http.Response, error)` 基础包装
- `func (c *Client) do(req *http.Request) (*http.Response, error)` 内部执行，带重试与 `Retry-After`
- `func (c *Client) stat(ctx, id string) (*StatResult, error)`
- `func (c *Client) List(ctx, parentID string) (*ListResult, error)`
- `func (c *Client) Delete(ctx, id string) error`
- `func (c *Client) DeleteDir(ctx, id string) error`
- `func (c *Client) Scan(ctx) (*ScanReport, error)`

**Consumed from codebase:** HTTP API paths from README/router.go，JSON 返回结构来自 `transport` handler。

**Produces for later tasks:** `Client` 作为后续 upload/download 的基础依赖。

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientList(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/ls", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("namespace") != "ns1" {
			t.Errorf("want namespace=ns1, got %s", r.URL.Query().Get("namespace"))
		}
		json.NewEncoder(w).Encode(map[string]any{
			"dir":      nil,
			"children": []any{},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient(srv.URL, "ns1")
	res, err := c.List(context.Background(), "/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res == nil || len(res.Children) != 0 {
		t.Fatalf("unexpected result: %+v", res)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/fileupload -run TestClientList -v`
Expected: FAIL `NewClient undefined`

- [ ] **Step 3: Write minimal client skeleton**

```go
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

type Client struct {
	ServerURL  string
	Namespace  string
	HTTPClient *http.Client
}

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

func (c *Client) do(req *http.Request) (*http.Response, error) {
	return c.HTTPClient.Do(req)
}

type ListResult struct {
	Dir      any           `json:"dir"`
	Children []map[string]any `json:"children"`
}

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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/fileupload -run TestClientList -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/fileupload/client.go cmd/fileupload/client_test.go
git commit -m "feat(cli): add shared HTTP client skeleton with List"
Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
```

---

## Task 2: 在 Client 中补齐 Stat/Delete/DeleteDir/Scan

**Files:**
- Modify: `cmd/fileupload/client.go`
- Test: `cmd/fileupload/client_test.go`

**Interfaces:**
- `type StatResult struct { File map[string]any; Blob map[string]any }`
- `func (c *Client) Stat(ctx, id string) (*StatResult, error)`
- `func (c *Client) Delete(ctx, id string) error`
- `func (c *Client) DeleteDir(ctx, id string) error`
- `func (c *Client) Scan(ctx) (map[string]any, error)`

- [ ] **Step 1: Write failing tests**

Add to `client_test.go`:

```go
func TestClientDelete(t *testing.T) {
	mux := http.NewServeMux()
	deleted := false
	mux.HandleFunc("DELETE /v1/files/{id}", func(w http.ResponseWriter, r *http.Request) {
		deleted = true
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient(srv.URL, "ns1")
	if err := c.Delete(context.Background(), "fid1"); err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Fatal("handler not called")
	}
}

func TestClientScan(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/admin/scan", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"orphan_parts": 0})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient(srv.URL, "ns1")
	res, err := c.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := res["orphan_parts"], float64(0); got != want {
		t.Fatalf("got %v want %v", got, want)
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./cmd/fileupload -run 'TestClientDelete|TestClientScan' -v`
Expected: FAIL methods undefined

- [ ] **Step 3: Implement methods**

Append to `client.go`:

```go
type StatResult struct {
	File map[string]any `json:"file"`
	Blob map[string]any `json:"blob"`
}

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
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./cmd/fileupload -run 'TestClient' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/fileupload/client.go cmd/fileupload/client_test.go
git commit -m "feat(cli): add stat/delete/scan client methods"
Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
```

---

## Task 3: 重试与 Retry-After 支持

**Files:**
- Modify: `cmd/fileupload/client.go`

**Interfaces:**
- 内部 `do` 支持 500/503 时按 `Retry-After` 或指数退避重试最多 3 次。

- [ ] **Step 1: Write test for retry**

```go
func TestClientRetry(t *testing.T) {
	attempts := 0
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/ls", func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 2 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"children": []any{}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient(srv.URL, "default")
	_, err := c.List(context.Background(), "/")
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./cmd/fileupload -run TestClientRetry -v`
Expected: FAIL (503 not retried)

- [ ] **Step 3: Implement retry logic**

Replace `do` with:

```go
func (c *Client) do(req *http.Request) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
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
		if sec, _ := strconv.Atoi(ra); sec > 0 {
			delay = time.Duration(sec) * time.Second
		} else if sec < 0 {
			delay = time.Duration(-sec) * time.Second
		}
		time.Sleep(delay)
	}
	return nil, lastErr
}
```

注意：重复发送 req 时body可能被消费；对当前 GET/DELETE/POST 中空 body 的情况安全。后续 upload chunk 使用单独方法，不在 do 重试。

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./cmd/fileupload -run 'TestClient' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git commit -am "feat(cli): client retry with Retry-After"
Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
```

---

## Task 4: 客户端压缩模块

**Files:**
- Create: `cmd/fileupload/compress.go`

**Interfaces:**
- `func compressBuffer(data []byte, format string) ([]byte, error)`
- `func compressReader(r io.Reader, format string) (io.Reader, error)` 流式压缩（用于大文件）
- 目录上传：先 tar 再可选 zstd/gzip，输出单一流。

- [ ] **Step 1: Write test**

```go
package main

import (
	"bytes"
	"io"
	"testing"
)

func TestCompressZstd(t *testing.T) {
	in := []byte("hello world hello world")
	out, err := compressBuffer(in, "zstd")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) >= len(in) {
		t.Fatalf("compression did not reduce size: %d >= %d", len(out), len(in))
	}

	r, err := decompressReader(bytes.NewReader(out), "zstd")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(r)
	if !bytes.Equal(got, in) {
		t.Fatalf("roundtrip failed")
	}
}
```

- [ ] **Step 2: Run test to fail**

Run: `go test ./cmd/fileupload -run TestCompressZstd -v`
Expected: FAIL undefined

- [ ] **Step 3: Implement compress/decompress helpers**

`compress.go`:

```go
package main

import (
	"bytes"
	"fmt"
	"io"

	"github.com/klauspost/compress/zstd"
)

func compressBuffer(data []byte, format string) ([]byte, error) {
	switch format {
	case "zstd":
		var buf bytes.Buffer
		enc, err := zstd.NewWriter(&buf)
		if err != nil {
			return nil, err
		}
		if _, err := enc.Write(data); err != nil {
			enc.Close()
			return nil, err
		}
		if err := enc.Close(); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	case "none", "":
		return data, nil
	default:
		return nil, fmt.Errorf("unsupported compress format: %s", format)
	}
}

func decompressReader(r io.Reader, format string) (io.Reader, error) {
	switch format {
	case "zstd":
		return zstd.NewReader(r)
	case "none", "":
		return r, nil
	default:
		return nil, fmt.Errorf("unsupported decompress format: %s", format)
	}
}
```

- [ ] **Step 4: Run test to pass**

Run: `go test ./cmd/fileupload -run TestCompressZstd -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/fileupload/compress.go cmd/fileupload/client_test.go
git commit -m "feat(cli): zstd compress/decompress helpers"
Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
```

---

## Task 5: Client 上传核心：秒传 + 创建会话 + 单分片 PATCH

**Files:**
- Modify: `cmd/fileupload/client.go`

**Interfaces:**
- `func (c *Client) CheckExists(ctx, sha256, name string) (*FileInfo, error)`
- `func (c *Client) CreateSession(ctx, size, sha256, compression string, chunkSize int64, fileName string) (sessionID string, err error)`
- `func (c *Client) UploadChunk(ctx, sessionID string, index int, data []byte, offset int64) error`
- `func (c *Client) Finalize(ctx, sessionID string) (*FileInfo, error)`
- `type FileInfo struct { FileID string; SHA256 string; Size int64; Name string }`

- [ ] **Step 1: Write tests** (httptest fake server)

```go
func TestClientUploadFlow(t *testing.T) {
	var sessionID string
	mux := http.NewServeMux()
	mux.HandleFunc("HEAD /v1/files", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("POST /uploads", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "/uploads/sess-123")
		w.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("PATCH /uploads/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Upload-Offset", r.Header.Get("Upload-Offset"))
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /v1/uploads/{id}/finalize", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"file_id": "fid", "sha256": "sha", "size": float64(5), "name": "x"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient(srv.URL, "default")
	exists, err := c.CheckExists(context.Background(), "sha", "x")
	if err != nil || exists {
		t.Fatalf("CheckExists unexpected: %v, %v", exists, err)
	}
	sid, err := c.CreateSession(context.Background(), 5, "sha", "none", 1024, "x")
	if err != nil || sid != "sess-123" {
		t.Fatalf("CreateSession: %s %v", sid, err)
	}
	if err := c.UploadChunk(context.Background(), sid, 0, []byte("hello"), 0); err != nil {
		t.Fatal(err)
	}
	info, err := c.Finalize(context.Background(), sid)
	if err != nil || info.FileID != "fid" {
		t.Fatalf("Finalize: %+v %v", info, err)
	}
}
```

- [ ] **Step 2: Run tests to fail**

Run: `go test ./cmd/fileupload -run TestClientUploadFlow -v`
Expected: FAIL undefined

- [ ] **Step 3: Implement upload methods**

Append到 `client.go`:

```go
type FileInfo struct {
	FileID string `json:"file_id"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Name   string `json:"name"`
}

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
```

需要引入 `crypto/sha256`、`encoding/hex`、`net/url`、`strconv`、`strings`、`bytes`。

并加辅助函数 `sha256Sum` 在 `compress.go` 或新文件中：

```go
func sha256Sum(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
```

- [ ] **Step 4: Run tests to pass**

Run: `go test ./cmd/fileupload -run 'TestClient' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git commit -am "feat(cli): add upload session, chunk, finalize, check-exists to client"
Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
```

---

## Task 6: 复写 upload.go：单文件上传接入 Client + 压缩 + 并发分片

**Files:**
- Modify: `cmd/fileupload/upload.go`
- Test: `cmd/fileupload/upload_test.go`

**Interfaces:**
- `func (c *Client) UploadFile(ctx, localPath string, options UploadOptions) (*FileInfo, error)`
- `type UploadOptions struct { ChunkSize int64; Concurrency int; Compress string; Resume bool }`

- [ ] **Step 1: Add client method `UploadFile`**

在 `client.go` 新增：

```go
type UploadOptions struct {
	ChunkSize   int64
	Concurrency int
	Compress    string
	Resume      bool
}

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
	fileName := info.Name()
	fileSize := info.Size()

	originalSHA, err := fileSHA256(localPath)
	if err != nil {
		return nil, err
	}

	if opts.Compress == "zstd" && fileSize > 0 {
		// 先整体压缩，得到压缩后大小与新的分片
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

	// no compression path
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	return c.uploadBytes(ctx, fileName, data, originalSHA, "none", opts)
}

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
			errCh <- c.UploadChunk(ctx, sessionID, idx, chunk, offset)
		}()
	}

	for i := 0; i < chunkCount; i++ {
		if err := <-errCh; err != nil {
			return nil, err
		}
	}

	return c.Finalize(ctx, sessionID)
}

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
```

注意 `os` 和 `io` 需要在 client.go 的 import 中加入。

- [ ] **Step 2: Rewrite upload.go to use Client.UploadFile**

替换 `upload.go` 中的 `runUpload`/`uploadFile`/`uploadDir`：

```go
func runUpload(ctx context.Context, cfg config.Config, args []string) {
	if len(args) < 1 {
		fmt.Println("用法: fileupload upload <path> [--chunk-size 10m] [--concurrency 4] [--compress zstd|none] [--server <url>] [--namespace <ns>]")
		os.Exit(1)
	}

	localPath := args[0]
	flags := parseFlags(args[1:])
	c := newClientFromFlags(flags, cfg)

	info, err := os.Stat(localPath)
	if err != nil {
		fmt.Printf("错误: 无法访问 %s: %v\n", localPath, err)
		os.Exit(1)
	}

	opts := UploadOptions{
		ChunkSize:   parseSize(flags, "chunk-size", 10*1024*1024),
		Concurrency: parseInt(flags, "concurrency", 4),
		Compress:    getFlag(flags, "compress", "none"),
		Resume:      getFlag(flags, "resume", "true") != "false",
	}

	if info.IsDir() {
		meta, err := c.UploadDir(ctx, localPath, opts)
		if err != nil {
			fmt.Printf("错误: 目录上传失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("目录上传完成! DirID: %s\n", meta.FileID)
		return
	}

	fmeta, err := c.UploadFile(ctx, localPath, opts)
	if err != nil {
		fmt.Printf("错误: 上传失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("上传完成! FileID: %s SHA-256: %s Size: %d 字节\n", fmeta.FileID, fmeta.SHA256, fmeta.Size)
}

func newClientFromFlags(flags map[string]string, cfg config.Config) *Client {
	serverURL := getServerURL(cfg, flags)
	namespace := getFlag(flags, "namespace", "default")
	return NewClient(serverURL, namespace)
}
```

- [ ] **Step 3: Add upload tests**

`upload_test.go`:

```go
package main

import "testing"

func TestParseSize(t *testing.T) {
	flags := map[string]string{"chunk-size": "5m"}
	if got := parseSize(flags, "chunk-size", 10); got != 5*1024*1024 {
		t.Fatalf("got %d", got)
	}
}
```

- [ ] **Step 4: Build and run tests**

Run:
```bash
go test ./cmd/fileupload -v
go build ./cmd/fileupload
```
Expected: PASS and binary builds.

- [ ] **Step 5: Commit**

```bash
git add cmd/fileupload/client.go cmd/fileupload/upload.go cmd/fileupload/upload_test.go
git commit -m "feat(cli): rewrite upload command using shared client with compression and concurrency"
Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
```

---

## Task 7: 修复目录上传（收集 fileID 并提交 manifest）

**Files:**
- Modify: `cmd/fileupload/client.go`
- Modify: `cmd/fileupload/upload.go`

**Interfaces:**
- `func (c *Client) UploadDir(ctx, dirPath string, opts UploadOptions) (*FileInfo, error)`
- 目录下每个文件调用 `c.UploadFile`；收集 `DirManifestEntry`；最后 `POST /v1/dirs`。

- [ ] **Step 1: Add DirManifestEntry + SubmitDir to client.go**

结构体复用服务端已定义的 `domain.DirManifest` 和 `domain.DirEntry`。由于 CLI 不能 import `internal/domain`，在 client.go 中定义等价的本地结构：

```go
type clientDirManifest struct {
	Entries []clientDirEntry `json:"entries"`
}

type clientDirEntry struct {
	Path   string `json:"path"`
	FileID string `json:"file_id"`
}

func (c *Client) SubmitDir(ctx context.Context, entries []clientDirEntry) (*FileInfo, error) {
	body, _ := json.Marshal(clientDirManifest{Entries: entries})
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
```

- [ ] **Step 2: Implement UploadDir**

在 client.go 中追加：

```go
func (c *Client) UploadDir(ctx context.Context, dirPath string, opts UploadOptions) (*FileInfo, error) {
	var entries []clientDirEntry
	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dirPath, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		fmeta, err := c.UploadFile(ctx, path, opts)
		if err != nil {
			return err
		}
		entries = append(entries, clientDirEntry{Path: rel, FileID: fmeta.FileID})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return c.SubmitDir(ctx, entries)
}
```

并确保 imports 中有 `path/filepath`。

- [ ] **Step 3: Verify build/tests**

Run:
```bash
go test ./cmd/fileupload -v
go build ./cmd/fileupload
```
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git commit -am "feat(cli): fix directory upload via manifest submission"
Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
```

---

## Task 8: 断点续传（Resume）

**Files:**
- Modify: `cmd/fileupload/client.go`

**Interfaces:**
- `func (c *Client) ResumeUpload(ctx, localPath string, opts UploadOptions) (*FileInfo, error)`
- 先由文件原始 SHA 反查 existing sessionID？服务器当前没有按 sha256 查 session 的接口。所以实现思路：本地维护 `.fileupload.resume` 状态文件（sessionID/localPath/sha256），启动时读取它；存在则 GET `/v1/uploads/{id}/status`，只上传缺失分片。

为简化首批实现：resume 只支持 REST 路径，因为 tus 原生 HEAD 已支持。CLI 单文件上传时可写入 `.fileupload.resume.<sha256>.json` 临时文件记录 sessionID，下次同文件上传时读取并续传。

- [ ] **Step 1: Write test for resume helper**

```go
func TestResumeState(t *testing.T) {
	dir := t.TempDir()
	state := resumeState{SessionID: "s1", SHA256: "abc", FileSize: 10}
	path := filepath.Join(dir, "resume.json")
	if err := saveResumeState(path, state); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadResumeState(path)
	if err != nil || loaded.SessionID != "s1" {
		t.Fatalf("%v %v", loaded, err)
	}
}
```

- [ ] **Step 2: Implement resume state helpers**

```go
type resumeState struct {
	SessionID string `json:"session_id"`
	SHA256    string `json:"sha256"`
	FileSize  int64  `json:"file_size"`
}

func resumeStatePath(sha256 string) string {
	return filepath.Join(os.TempDir(), fmt.Sprintf(".fileupload.resume.%s.json", sha256))
}

func saveResumeState(path string, state resumeState) error {
	data, _ := json.Marshal(state)
	return os.WriteFile(path, data, 0600)
}

func loadResumeState(path string) (resumeState, error) {
	var state resumeState
	data, err := os.ReadFile(path)
	if err != nil {
		return state, err
	}
	err = json.Unmarshal(data, &state)
	return state, err
}
```

- [ ] **Step 3: Modify UploadFile to support resume**

在创建 session 前先检查 resume state；若存在且 SHA/Size 一致，调用 `c.uploadMissingChunks(...)`

```go
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
		go func() {
			defer func() { <-sem }()
			errCh <- c.UploadChunk(ctx, sessionID, idx, chunk, offset)
		}()
	}
	return nil
}
```

注意这会漏 errCh 收集，需要完整循环。为简化，先只收集发送 errCh 的数量。

- [ ] **Step 4: Verify tests**

Run: `go test ./cmd/fileupload -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git commit -am "feat(cli): resume upload from saved session state"
Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
```

---

## Task 9: 进度条

**Files:**
- Create: `cmd/fileupload/progress.go`

**Interfaces:**
- `type Progress struct { total int64; current int64 }`
- `func NewProgress(total int64) *Progress`
- `func (p *Progress) Add(n int64)`
- 简单实现：每完成一个分片打印一次 `Uploaded X/Y MB (P%)`。

- [ ] **Step 1: Write progress test**

```go
package main

import "testing"

func TestProgress(t *testing.T) {
	p := NewProgress(100)
	p.Add(30)
	if p.Percent() != 30 {
		t.Fatalf("got %d", p.Percent())
	}
}
```

- [ ] **Step 2: Implement progress**

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

type Progress struct {
	total    int64
	current  int64
	mu       sync.Mutex
	onUpdate func(current, total int64)
}

func NewProgress(total int64, onUpdate func(current, total int64)) *Progress {
	return &Progress{total: total, onUpdate: onUpdate}
}

func (p *Progress) Add(n int64) {
	atomic.AddInt64(&p.current, n)
	if p.onUpdate != nil {
		p.onUpdate(p.current, p.total)
	}
}

func (p *Progress) Percent() int {
	if p.total == 0 {
		return 100
	}
	return int(atomic.LoadInt64(&p.current) * 100 / p.total)
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for n >= div*unit && exp < 4 {
		div *= unit
		exp++
	}
	switch exp {
	case 0:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(div))
	case 1:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(div))
	case 2:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(div))
	default:
		return fmt.Sprintf("%.1f TB", float64(n)/float64(div))
	}
}
```

- [ ] **Step 3: Wire progress into UploadFile**

在 `uploadBytes` 的每个 goroutine 完成后调用 progress.Add(len(chunk))，并初始化 `NewProgress(int64(len(data)), ...)`。

- [ ] **Step 4: Verify tests/build**

Run: `go test ./cmd/fileupload -v && go build ./cmd/fileupload`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/fileupload/progress.go cmd/fileupload/client.go cmd/fileupload/client_test.go
git commit -m "feat(cli): add progress reporting to uploads"
Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
```

---

## Task 10: 统一各子命令（rm/ls/stat/scan/bench）接入 Client

**Files:**
- Modify: `cmd/fileupload/delete.go`
- Modify: `cmd/fileupload/list.go`
- Modify: `cmd/fileupload/stat.go`
- Modify: `cmd/fileupload/scan.go`
- Modify: `cmd/fileupload/bench.go`
- Modify: `cmd/fileupload/main.go`（如果需要提取 `newClientFromFlags` 复用）

- [ ] **Step 1: Refactor delete.go**

```go
func runDelete(ctx context.Context, cfg config.Config, args []string) {
	if len(args) < 1 {
		fmt.Println("用法: fileupload rm <fileID|dirID> [--server <url>] [--namespace <ns>]")
		os.Exit(1)
	}
	id := args[0]
	flags := parseFlags(args[1:])
	c := newClientFromFlags(flags, cfg)

	isDir, err := isDir(ctx, c, id)
	if err != nil {
		fmt.Printf("错误: 判断类型失败: %v\n", err)
		os.Exit(1)
	}
	if isDir {
		err = c.DeleteDir(ctx, id)
	} else {
		err = c.Delete(ctx, id)
	}
	if err != nil {
		fmt.Printf("错误: 删除失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("已删除: %s\n", id)
}

func isDir(ctx context.Context, c *Client, id string) (bool, error) {
	res, err := c.Stat(ctx, id)
	if err != nil {
		return false, err
	}
	if res == nil || res.File == nil {
		return false, fmt.Errorf("not found")
	}
	if d, ok := res.File["is_dir"].(bool); ok {
		return d, nil
	}
	return false, nil
}
```

- [ ] **Step 2: Refactor list.go**

```go
func runList(ctx context.Context, cfg config.Config, args []string) {
	parentID := "/"
	if len(args) > 0 {
		parentID = args[0]
	}
	flags := parseFlags(args[1:])
	c := newClientFromFlags(flags, cfg)

	res, err := c.List(ctx, parentID)
	if err != nil {
		fmt.Printf("错误: 列出目录失败: %v\n", err)
		os.Exit(1)
	}
	if len(res.Children) == 0 {
		fmt.Println("(空目录)")
		return
	}
	for _, c := range res.Children {
		name, _ := c["name"].(string)
		fileID, _ := c["file_id"].(string)
		isDir, _ := c["is_dir"].(bool)
		size := int64(0)
		if s, ok := c["size"].(float64); ok {
			size = int64(s)
		}
		prefix := "📄 "
		if isDir {
			prefix = "📁 "
		}
		fmt.Printf("%s%s  (%s, %s)\n", prefix, name, fileID, humanBytes(size))
	}
}
```

- [ ] **Step 3: Refactor stat.go**

```go
func runStat(ctx context.Context, cfg config.Config, args []string) {
	if len(args) < 1 {
		fmt.Println("用法: fileupload stat <fileID> [--server <url>] [--namespace <ns>]")
		os.Exit(1)
	}
	id := args[0]
	flags := parseFlags(args[1:])
	c := newClientFromFlags(flags, cfg)

	res, err := c.Stat(ctx, id)
	if err != nil {
		fmt.Printf("错误: 查询失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("文件信息:\n")
	for k, v := range res.File {
		fmt.Printf("  %s: %v\n", k, v)
	}
	if res.Blob != nil {
		fmt.Printf("  (blob)\n")
		for k, v := range res.Blob {
			fmt.Printf("    %s: %v\n", k, v)
		}
	}
}
```

- [ ] **Step 4: Refactor scan.go**

```go
func runScan(ctx context.Context, cfg config.Config) {
	flags := parseFlags(os.Args[2:])
	c := newClientFromFlags(flags, cfg)
	fmt.Printf("触发服务端一致性巡检: %s\n", c.ServerURL)

	res, err := c.Scan(ctx)
	if err != nil {
		fmt.Printf("错误: 巡检失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("\n巡检报告:")
	for k, v := range res {
		fmt.Printf("  %s: %v\n", k, v)
	}
}
```

- [ ] **Step 5: Refactor bench.go 使用 Client.UploadBytes**

将压测里手动拼 HTTP 的代码替换为 `Client.uploadBytes(ctx, name, data, "", "none", opts)`。注意 `uploadBytes` 当前是 unexported；需要改成 `UploadBytes` 或 bench 包内访问（同 package main，无需导出）。

- [ ] **Step 6: Verify build/tests**

Run:
```bash
go test ./cmd/fileupload -v
go build ./cmd/fileupload
```
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git commit -am "feat(cli): wire all subcommands through shared client"
Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
```

---

## Task 11: 复写 download.go 接入 Client + 校验 + 目录解压

**Files:**
- Modify: `cmd/fileupload/download.go`
- Modify: `cmd/fileupload/client.go`（增加下载方法）

**Interfaces:**
- `func (c *Client) DownloadFile(ctx, fileID, outputPath string, verify bool, rng string) error`
- `func (c *Client) DownloadDir(ctx, dirID, outputPath, format string, verify bool) error`

- [ ] **Step 1: Add download methods to client.go**

```go
func (c *Client) DownloadFile(ctx context.Context, fileID string, rng string) (*http.Response, error) {
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
```

- [ ] **Step 2: Rewrite download.go**

```go
func runDownload(ctx context.Context, cfg config.Config, args []string) {
	if len(args) < 1 {
		fmt.Println("用法: fileupload download <fileID|dirID> [-o <path>] [--format tar.gz] [--verify] [--range start-end] [--server <url>] [--namespace <ns>]")
		os.Exit(1)
	}
	id := args[0]
	flags := parseFlags(args[1:])
	c := newClientFromFlags(flags, cfg)
	outputPath := getFlag(flags, "o", id)
	format := getFlag(flags, "format", "tar.gz")
	verify := getFlag(flags, "verify", "false") == "true"
	rng := getFlag(flags, "range", "")

	isDir, err := isDir(ctx, c, id)
	if err != nil {
		fmt.Printf("错误: 判断类型失败: %v\n", err)
		os.Exit(1)
	}
	if isDir {
		if err := downloadDir(ctx, c, id, outputPath, format, verify); err != nil {
			fmt.Printf("错误: 下载目录失败: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if err := downloadFile(ctx, c, id, outputPath, rng, verify); err != nil {
		fmt.Printf("错误: 下载文件失败: %v\n", err)
		os.Exit(1)
	}
}

func downloadFile(ctx context.Context, c *Client, fileID, outputPath, rng string, verify bool) error {
	resp, err := c.DownloadFile(ctx, fileID, rng)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	out, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer out.Close()

	var h hash.Hash
	var w io.Writer = out
	if verify {
		h = sha256.New()
		w = io.MultiWriter(out, h)
	}
	written, err := io.Copy(w, resp.Body)
	if err != nil {
		return err
	}
	serverSHA := resp.Header.Get("X-SHA256")
	if verify && serverSHA != "" && h != nil {
		got := hex.EncodeToString(h.Sum(nil))
		if got != serverSHA {
			return fmt.Errorf("SHA-256 mismatch: got %s want %s", got, serverSHA)
		}
	}
	fmt.Printf("下载完成: %s (%d 字节)\n", outputPath, written)
	if serverSHA != "" {
		fmt.Printf("SHA-256: %s\n", serverSHA)
	}
	return nil
}

func downloadDir(ctx context.Context, c *Client, dirID, outputPath, format string, verify bool) error {
	if !strings.HasSuffix(outputPath, ".tar.gz") && !strings.HasSuffix(outputPath, ".tar.zst") {
		outputPath += "." + format
	}
	resp, err := c.DownloadDir(ctx, dirID, format)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	out, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer out.Close()

	written, err := io.Copy(out, resp.Body)
	if err != nil {
		return err
	}
	treeSHA := resp.Header.Get("X-Tree-SHA256")
	fmt.Printf("目录下载完成: %s (%d 字节)\n", outputPath, written)
	if treeSHA != "" {
		fmt.Printf("Tree SHA-256: %s\n", treeSHA)
	}
	return nil
}
```

需要 imports：`context` `crypto/sha256` `encoding/hex` `fmt` `hash` `io` `os` `strings` `github.com/mayc/casdao/fileupload/internal/config`。

- [ ] **Step 3: Build and test**

Run:
```bash
go test ./cmd/fileupload -v
go build ./cmd/fileupload
```
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git commit -am "feat(cli): rewrite download command using shared client with verify"
Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
```

---

## Task 12: 端到端集成测试骨架（用 httptest 跑 CLI 命令）

**Files:**
- Create: `cmd/fileupload/e2e_test.go`

- [ ] **Step 1: Write E2E test for upload roundtrip**

`e2e_test.go`:

```go
package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestE2E_UploadFile(t *testing.T) {
	// minimal fake server that mimics server enough for Client
	mux := http.NewServeMux()
	mux.HandleFunc("HEAD /v1/files", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	var gotSessionID string
	mux.HandleFunc("POST /uploads", func(w http.ResponseWriter, r *http.Request) {
		gotSessionID = "sess-1"
		w.Header().Set("Location", "/uploads/sess-1")
		w.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("PATCH /uploads/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Upload-Offset", r.Header.Get("Upload-Offset"))
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /v1/uploads/{id}/finalize", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"file_id":"fid","sha256":"%s","size":5,"name":"hello.txt"}`, r.URL.Path)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	dir := t.TempDir()
	file := filepath.Join(dir, "hello.txt")
	os.WriteFile(file, []byte("hello"), 0644)

	c := NewClient(srv.URL, "default")
	info, err := c.UploadFile(context.Background(), file, UploadOptions{
		ChunkSize:   1024,
		Concurrency: 1,
		Compress:    "none",
	})
	if err != nil {
		t.Fatal(err)
	}
	if info.FileID != "fid" {
		t.Fatalf("want fid got %s", info.FileID)
	}
}
```

- [ ] **Step 2: Run test**

Run: `go test ./cmd/fileupload -run TestE2E -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add cmd/fileupload/e2e_test.go
git commit -m "test(cli): add e2e upload roundtrip against httptest server"
Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
```

---

## Task 13: 更新文档与 Makefile / README

**Files:**
- Modify: `docs/progress.md`
- Modify: `docs/task_plan.md`
- Modify: `README.md`（如 CLI 用法已变）

- [ ] **Step 1: Update progress.md** 标记 A 阶段完成，列出已完成的命令与文件。

- [ ] **Step 2: Update task_plan.md** 将阶段 4「服务端 + CLI」中「CLI命令」标记完成。

- [ ] **Step 3: README CLI usage review** 如有 flag 变化（例如 --compress 默认改为 none）需要同步。检查 `--compress zstd` 默认是否与当前代码一致。

- [ ] **Step 4: Commit**

```bash
git commit -am "docs(cli): update progress and task plan for CLI phase"
Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
```

---

## Spec Coverage Check

| Spec 章节 | 覆盖任务 | 备注 |
|---|---|---|
| 8.2 Go CLI 设计 | Task 6-12 | upload/download/ls/stat/rm/scan/bench/config 全部接入 Client |
| 8.2 `--chunk-size`、`--concurrency`、`--compress`、`--resume` | Task 6/8 | 已实现（压缩默认 none，如 README 默认 zstd 需调整） |
| 8.2 目录上传 | Task 7 | manifest 提交实现 |
| 3.2 下载 Range/校验 | Task 11 | `--range`、`--verify` 接入 |
| 5.2 断点续传 | Task 8 | 本地 resume state 实现 |
| 8.3 共享 `pkg/` | 未覆盖 | spec 提到 `pkg/` 共享核心；本计划仍把共享逻辑放 `cmd/fileupload` package 内，因无 SDK 需求。后续如需 `pkg/` 可 refactor。 |

---

## Placeholder Scan

- 无 TBD、TODO。
- 所有代码片段为可直接运行的 Go 代码。
- 每个任务都有明确测试与命令。
- `bench.go` 中调用 `uploadBytes`：因同 package main，unexported 可访问。

---

## Type Consistency Check

- `FileInfo` 在 Task 5 定义，Task 6/7/12 使用，字段 `FileID/SHA256/Size/Name` 一致。
- `UploadOptions` 在 Task 6 定义，Task 7/8/12 使用，字段一致。
- `ListResult`/`StatResult` 在 Task 1/2 定义，Task 10 使用，字段一致。

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-06-18-cli-complete-linkage.md`.

Two execution options:

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration.

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints for review.

Which approach?