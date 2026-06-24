# fileupload SDK 使用指南

`fileupload` 提供 Go 与 JavaScript 两套 SDK，覆盖所有 REST + tus 端点。每个方法附 Go / JS / curl 三种用法示例。

> **TL;DR**
> ```go
> // Go
> c := fileupload.NewClient("http://localhost:8080", "default")
> info, err := c.Upload(ctx, "/path/to/file.txt")
> ```
> ```ts
> // JS / TS
> const client = new FileuploadClient({ endpoint: 'http://localhost:8080' })
> const info = await client.upload(fileBlob, 'photo.jpg')
> ```

---

## 安装

### Go
```bash
go get github.com/bilbilmyc/fileupload/sdk/go/fileupload
```

### JavaScript / TypeScript
```bash
npm install @fileupload/sdk
# 或 pnpm / yarn
```

---

## 客户端初始化

### Go
```go
c := fileupload.NewClient("http://localhost:8080", "default")
// 可选：自定义 token / 命名空间 / HTTP 客户端
c.SetToken("eyJ...")
c.SetNamespace("my-ns")
c.SetHTTPClient(&http.Client{Timeout: 30 * time.Second})
```

### JavaScript
```ts
const client = new FileuploadClient({
  endpoint: 'http://localhost:8080',
  namespace: 'my-ns',
})
client.setToken('eyJ...')
```

### curl 等价物
```bash
curl -X POST http://localhost:8080/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"alice","password":"secret"}'
```

---

## API 参考

### 1. 鉴权

| 方法 | Go | JS |
|------|----|----|
| 登录 | `Login(ctx, user, pass)` | `login(user, pass)` |
| 刷新 token | `RefreshToken(ctx, refresh)` | `refreshToken(refresh)` |
| 当前用户 | `Me(ctx)` | `me()` |

#### 示例

**Go**
```go
pair, err := c.Login(ctx, "alice", "secret")
// pair.AccessToken / RefreshToken / ExpiresIn
// token 已自动设置到 c，后续请求带 Authorization

me, err := c.Me(ctx)
// me.UserID / Namespace / Roles
```

**JS**
```ts
const pair = await client.login('alice', 'secret')
// 自动 setToken
const me = await client.me()
```

**curl**
```bash
curl -X POST .../v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"alice","password":"secret"}'
```

---

### 2. 上传

#### 单文件上传（tus 协议）

**Go**
```go
info, err := c.Upload(ctx, "/path/to/photo.jpg",
    fileupload.WithFileName("photo.jpg"),
    fileupload.WithConcurrency(4),
    fileupload.WithCompression("zstd"),
)
// info.FileID / SHA256 / Size / Name
```

**JS（浏览器 / Blob）**
```ts
const blob = fileInput.files[0]
const info = await client.upload(blob, 'photo.jpg')
```

**JS（Node.js / Buffer）**
```ts
import { readFileSync } from 'fs'
const buf = readFileSync('photo.jpg')
const info = await client.upload(new Blob([buf]), 'photo.jpg')
```

#### 秒传预检

**Go**
```go
existing, err := c.Upload(ctx, "/path/to/file.txt",
    fileupload.WithSHA256(sha256Hex))
if existing == nil { /* 需上传 */ }
```

**JS**
```ts
const existing = await client.checkExists(sha256, 'file.txt')
if (existing === null) { /* 需上传 */ }
```

#### 上传分片状态查询

**Go**
```go
status, err := c.GetUploadStatus(ctx, sessionID)
// status.Chunks[i].Index / SHA256 / Size
// status.Total
```

**JS**
```ts
const status = await client.getUploadStatus(sessionID)
```

#### 取消上传

**Go**
```go
err := c.CancelUpload(ctx, sessionID)
```

**JS**
```ts
await client.cancelUpload(sessionID)
```

**curl**
```bash
curl -X DELETE .../uploads/{sessionID}
```

---

### 3. 下载

#### 单文件下载

**Go**
```go
resp, err := c.Download(ctx, fileID, "")  // rangeStr = "" 表示完整文件
defer resp.Body.Close()
io.Copy(outFile, resp.Body)
```

**JS**
```ts
const blob: Blob = await client.download(fileID)
```

**curl**
```bash
curl .../v1/files/{fileID} -o file.bin
```

#### 目录下载（zip / tar.gz）

**Go**
```go
resp, err := c.DownloadDir(ctx, dirID, "zip")
defer resp.Body.Close()
```

**JS**
```ts
const url = client.downloadDirUrl(dirID, 'zip')
window.location.href = url
```

#### 批量下载

**Go**
```go
resp, err := c.BatchDownload(ctx, []string{"id1", "id2"}, "tar.gz")
```

**JS**
```ts
const url = client.batchDownloadUrl(['id1', 'id2'], 'zip')
```

---

### 4. 预览

**Go**
```go
url := c.PreviewURL(fileID)         // 直接拿到 URL
// 或
resp, err := c.Preview(ctx, fileID) // 拿到 *http.Response，自行处理
defer resp.Body.Close()
```

**JS**
```ts
const blob = await client.preview(fileID)
// 或直接构造 URL：
const url = client.previewUrl(fileID)
```

**curl**
```bash
curl .../v1/preview/{id} -o preview.png
```

---

### 5. 目录管理

#### 列目录

**Go**
```go
list, err := c.List(ctx, parentID) // parentID = "" 表示根
// list.Dir / Children / Total / Ancestors
```

**JS**
```ts
const list = await client.list('/')
// list.dir / children / total / ancestors
```

#### 获取文件信息

**Go**
```go
stat, err := c.Stat(ctx, fileID)
// stat.File / Blob
```

**JS**
```ts
const { file, blob } = await client.stat(fileID)
```

#### 删除文件 / 目录

**Go**
```go
err := c.Delete(ctx, fileID)
err := c.DeleteDir(ctx, dirID)
```

**JS**
```ts
await client.delete(fileID)
await client.deleteDir(dirID)
```

#### 重命名

**Go**
```go
// TODO：v0.2.0+ 待补
```

**curl**
```bash
curl -X PATCH .../v1/files/{id} -H "Content-Type: application/json" -d '{"name":"newname"}'
```

#### 提交目录 Manifest

**Go**
```go
info, err := c.SubmitDir(ctx, "my-dir", []fileupload.clientDirEntry{
    {Path: "a.txt", FileID: "f1"},
    {Path: "b.txt", FileID: "f2"},
})
```

**JS**
```ts
const result = await client.submitDir({
  name: 'my-dir',
  entries: [
    { path: 'a.txt', file_id: 'f1' },
    { path: 'b.txt', file_id: 'f2' },
  ],
})
// result.file_id
```

**curl**
```bash
curl -X POST .../v1/dirs -H "Content-Type: application/json" \
  -d '{"name":"my-dir","entries":[{"path":"a.txt","file_id":"f1"}]}'
```

---

### 6. 批量操作

#### 批量删除

**Go**
```go
result, err := c.BatchDelete(ctx, []string{"id1", "id2"})
// result.Success / Failed
```

**JS**
```ts
const result = await client.batchDelete(['id1', 'id2'])
// { success, failed }
```

#### 批量复制

**Go**
```go
result, err := c.BatchCopy(ctx, []string{"id1", "id2"}, "target-dir")
// result.Success / Failed（v0.1.0+）
```

**JS**
```ts
const result = await client.batchCopy(['id1', 'id2'], 'target-dir')
// { success, failed }
```

#### 批量移动 / 标记

**JS**
```ts
await client.batchMove(['id1', 'id2'], 'target-dir')
await client.batchSetTags(['id1'], ['important'])
```

---

### 7. 后台 / 监控（v0.3.0+）

#### Prometheus 指标

```bash
curl http://localhost:8080/metrics
```

返回 Prometheus 文本格式，7 个核心指标：
- `fileupload_uploads_total{result}`
- `fileupload_upload_bytes_total`
- `fileupload_downloads_total{kind,result}`
- `fileupload_batch_operations_total{operation,result}`
- `fileupload_batch_operation_items_total{operation,item_result}`
- `fileupload_reaper_cleanups_total{kind}`
- `fileupload_health_status{component}`

详细监控规则见 `deploy/prometheus/alerts.yml`，Grafana 面板见 `deploy/grafana/dashboard.json`。

---

## 错误处理

### Go
非 200 响应返回 `*fileupload.StatusError`，包含 HTTP 状态码与响应体：

```go
info, err := c.Upload(ctx, path)
if err != nil {
    var se *fileupload.StatusError
    if errors.As(err, &se) {
        log.Printf("HTTP %d: %s", se.Code, se.Body)
    }
}
```

### JS
axios 默认 reject 非 2xx 响应，错误对象的 `response.data` 含服务端错误详情：

```ts
try {
  await client.upload(blob, 'a.txt')
} catch (err: any) {
  console.error(err.response?.status, err.response?.data)
}
```

---

## 完整示例

### Go：上传 + 下载一个文件

```go
package main

import (
    "context"
    "fmt"
    "os"

    "github.com/bilbilmyc/fileupload/sdk/go/fileupload"
)

func main() {
    ctx := context.Background()
    c := fileupload.NewClient("http://localhost:8080", "default")

    // 1. 上传
    info, err := c.Upload(ctx, "./report.pdf",
        fileupload.WithFileName("report.pdf"),
    )
    if err != nil {
        panic(err)
    }
    fmt.Printf("上传成功: %s (sha=%s, size=%d)\n", info.FileID, info.SHA256, info.Size)

    // 2. 下载回本地
    resp, err := c.Download(ctx, info.FileID, "")
    if err != nil {
        panic(err)
    }
    defer resp.Body.Close()

    f, _ := os.Create("./report.downloaded.pdf")
    defer f.Close()
    io.Copy(f, resp.Body)
}
```

### JS（浏览器）：批量上传 + 列表

```ts
import { FileuploadClient } from '@fileupload/sdk'

const client = new FileuploadClient({ endpoint: '/api' })

async function uploadAndList(files: FileList) {
  for (const file of Array.from(files)) {
    const info = await client.upload(file, file.name)
    console.log(`uploaded ${info.file_id}`)
  }
  const list = await client.list('/')
  console.log(`当前目录 ${list.total} 个文件`)
}

document.querySelector('input[type=file]')!
  .addEventListener('change', e => uploadAndList((e.target as HTMLInputElement).files!))
```

---

## 版本对应

| SDK 版本 | 服务端版本 | 主要差异 |
|---|---|---|
| 0.1.x | v0.1.0+ | 基础 CRUD + 批量 |
| 0.2.x | v0.2.0+ | + Auth / Cancel / Preview |
| 0.3.x | v0.3.0+ | + BatchCopyResult / StatusError / GetStatus |

---

## 贡献

- Go SDK：`sdk/go/fileupload/`
- JS SDK：`sdk/js/src/`
- 测试：`go test ./sdk/go/fileupload/...`
- TypeScript 校验：`cd sdk/js && npx tsc --noEmit`

新增 SDK 方法流程：
1. 在服务端实现 → 在 SDK 加方法
2. Go：写 httptest 测试 + 实现
3. JS：在 types.ts 加类型，在 client.ts 加方法
4. 在本文件加示例
5. 提交 PR