# API 参考文档

## 基础

**Base URL:** `http://<host>:8080`

**Namespace 隔离：** 所有 API 通过 `X-Namespace` 请求头或 `namespace` 查询参数隔离多租户，缺省为 `default`。

**内容类型：** 请求体为 `application/octet-stream` 或 `application/json`；响应体统一为 `application/json`。

---

## 上传流程

```
REST 协议:                              tus 协议:
  POST /v1/uploads/init                  POST /uploads
        │                                      │
  ┌─────▼──────┐                        ┌─────▼──────┐
  │ Upload      │  PUT /v1/uploads/      │ PATCH      │ PUT/PATCH chunks
  │ Chunks(s)  │  {id}/chunks/{index}   │ /uploads/  │ (并发或顺序)
  │             │  (每片独立请求)          │ {id}       │
  └─────┬──────┘                        └─────┬──────┘
        │                                      │
  ┌─────▼──────┐                        ┌─────▼──────┐
  │ Finalize   │  POST /v1/uploads/      │ Finalize   │
  │            │  {id}/finalize          │            │ (同一 REST 端点)
  └────────────┘                        └────────────┘
        │
  ┌─────▼──────────┐
  │ 服务端内部流程   │
  │ 1. 排序分片      │
  │ 2. 合并为临时文件 │
  │ 3. 解压（如 zstd）│
  │ 4. SHA-256 校验  │
  │ 5. 写入 data/    │
  │ 6. 清理临时分片   │
  └────────────────┘
```

---

## 上传 API

### POST /v1/uploads/init — 创建上传会话

**请求：**
```
POST /v1/uploads/init?size=10485760
X-SHA256: abcdef0123456789...
X-Compression: zstd
X-File-Name: example.dat
X-Namespace: my-ns
```

| 参数 | 位置 | 必填 | 说明 |
|------|------|------|------|
| `size` | query | 是 | 文件总字节数 |
| `X-SHA256` | header | 否 | 原始内容 SHA-256（用于秒传和 finalize 校验） |
| `X-Compression` | header | 否 | `none` / `zstd`，缺省 `none` |
| `X-File-Name` | header | 否 | 文件名（URL 编码的中文名自动解码） |

**响应 `201 Created`：**
```json
{
  "session_id": "a1b2c3d40000000000000001",
  "chunk_size": 10485760
}
```

---

### POST /uploads — 创建上传会话（tus 协议）

**请求：**
```
POST /uploads
Upload-Length: 10485760
X-SHA256: abcdef0123456789...
X-Compression: zstd
X-Chunk-Size: 10485760
X-File-Name: example.dat
X-Namespace: my-ns
```

**响应 `201 Created`：**
```
Location: /uploads/a1b2c3d40000000000000001
Upload-Offset: 0
```

---

### PUT /v1/uploads/{session_id}/chunks/{index} — 上传分片（REST）

**请求：**
```
PUT /v1/uploads/a1b2c3d40000000000000001/chunks/0
X-Slice-SHA256: feedface...
X-Namespace: my-ns
Content-Type: application/octet-stream

<binary chunk data>
```

**响应 `200 OK`**（空 body）

---

### PATCH /uploads/{session_id} — 上传分片（tus）

**请求：**
```
PATCH /uploads/a1b2c3d40000000000000001
Upload-Offset: 0
X-Slice-Index: 0
X-Slice-SHA256: feedface...
X-Namespace: my-ns
Content-Type: application/offset+octet-stream

<binary chunk data>
```

**响应 `204 No Content`：**
```
Upload-Offset: 1048576
```

---

### HEAD /uploads/{session_id} — 查询上传进度（tus）

**请求：**
```
HEAD /uploads/a1b2c3d40000000000000001
X-Namespace: my-ns
```

**响应 `200 OK`：**
```
Upload-Offset: 5242880
```

---

### GET /v1/uploads/{session_id}/status — 查询分片状态

**请求：**
```
GET /v1/uploads/a1b2c3d40000000000000001/status
X-Namespace: my-ns
```

**响应 `200 OK`：**
```json
{
  "session_id": "a1b2c3d40000000000000001",
  "chunks": [
    { "index": 0, "offset": 0, "size": 1048576 },
    { "index": 1, "offset": 1048576, "size": 1048576 }
  ],
  "total": 2097152
}
```

---

### POST /v1/uploads/{session_id}/finalize — 完成上传

**请求：**
```
POST /v1/uploads/a1b2c3d40000000000000001/finalize
X-Namespace: my-ns
```

**响应 `200 OK`：**
```json
{
  "file_id": "f1e2d3c40000000000000001",
  "sha256": "abcdef0123456789...",
  "size": 10485760,
  "name": "example.dat"
}
```

**服务端处理：**
1. 校验所有分片已上传
2. 按 index 排序、合并
3. 解压（若压缩格式为 zstd）
4. 计算最终 SHA-256 与声明的 `X-SHA256` 比对
5. 写入 `data/<namespace>/<file_id>`
6. 清理临时分片文件
7. 创建逻辑文件记录

---

### DELETE /uploads/{session_id} — 取消上传（tus）

**请求：**
```
DELETE /uploads/a1b2c3d40000000000000001
X-Namespace: my-ns
```

**响应 `204 No Content`：** 会话标记为 aborted，临时文件被清理。

---

## 下载 API

### GET /v1/files/{file_id} — 下载文件

**请求：**
```
GET /v1/files/f1e2d3c40000000000000001
X-Namespace: my-ns
```

**响应 `200 OK`：**
```
Content-Type: application/octet-stream
X-SHA256: abcdef0123456789...
Content-Disposition: attachment; filename="example.dat"
Content-Length: 10485760

<binary file data>
```

#### Range 分段下载

**请求：**
```
GET /v1/files/f1e2d3c40000000000000001
Range: bytes=0-1048575
X-Namespace: my-ns
```

**响应 `206 Partial Content`：**
```
Content-Range: bytes 0-1048575/10485760
Content-Length: 1048576

<first 1MB chunk>
```

---

### GET /v1/dirs/{dir_id} — 下载目录（流式打包）

**请求：**
```
GET /v1/dirs/d_a1b2c3d40000000000000001?format=tar.gz
X-Namespace: my-ns
```

**响应 `200 OK`：**
```
Content-Type: application/gzip
X-Tree-SHA256: deadbeef...
Content-Disposition: attachment; filename="dir.tar.gz"

<gzip stream>
```

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `format` | `tar.gz` | 打包格式：`tar.gz` / `tar.zst` |

---

## 秒传 API

### HEAD /v1/files — 秒传预检

**请求：**
```
HEAD /v1/files?sha256=abcdef0123456789...&name=example.dat
X-Namespace: my-ns
```

**命中（内容已存在）响应 `200 OK`：**
```json
{
  "file_id": "f1e2d3c40000000000000002",
  "sha256": "abcdef0123456789...",
  "size": 10485760
}
```
说明：引用计数 +1，返回新 file_id，客户端可直接认为上传完成。

**未命中响应 `404 Not Found`：** 客户端应继续上传。

---

## 目录管理 API

### POST /v1/dirs — 提交目录 Manifest

**请求：**
```
POST /v1/dirs
X-Namespace: my-ns
Content-Type: application/json

{
  "name": "my-project",
  "entries": [
    { "path": "src/main.go", "file_id": "f1e2d3c40000000000000001" },
    { "path": "src/utils.go", "file_id": "f1e2d3c40000000000000002" },
    { "path": "README.md", "file_id": "f1e2d3c40000000000000003" }
  ]
}
```

前置条件：entries 中的所有 file_id 必须通过前述上传流程（或秒传）已存在于服务端。

**响应 `201 Created`：**
```json
{
  "file_id": "d_a1b2c3d40000000000000001"
}
```

---

### GET /v1/ls — 列目录

**请求：**
```
GET /v1/ls?parent=/
X-Namespace: my-ns
```

**响应 `200 OK`：**
```json
{
  "dir": null,
  "children": [
    {
      "file_id": "d_a1b2c3d40000000000000001",
      "name": "my-project",
      "size": 0,
      "is_dir": true,
      "created_at": "2026-06-22T10:00:00+08:00"
    },
    {
      "file_id": "f1e2d3c40000000000000001",
      "name": "example.dat",
      "size": 10485760,
      "is_dir": false,
      "sha256": "abcdef0123456789...",
      "created_at": "2026-06-22T09:00:00+08:00"
    }
  ]
}
```

`parent` 参数：`/` 表示根目录，传入 `dir_id` 列出该目录的子文件。

---

### GET /v1/stat/{id} — 文件/目录元信息

**请求：**
```
GET /v1/stat/f1e2d3c40000000000000001
X-Namespace: my-ns
```

**响应 `200 OK`：**
```json
{
  "file": {
    "file_id": "f1e2d3c40000000000000001",
    "sha256": "abcdef0123456789...",
    "name": "example.dat",
    "size": 10485760,
    "namespace": "my-ns",
    "is_dir": false,
    "parent_id": "",
    "created_at": "2026-06-22T09:00:00+08:00"
  },
  "blob": {
    "sha256": "abcdef0123456789...",
    "storage_path": "my-ns/example.dat",
    "size": 10485760,
    "ref_count": 3,
    "created_at": "2026-06-22T08:00:00+08:00"
  }
}
```

`blob` 为内容寻址信息，仅在文件（非目录）时存在。`ref_count` 表示该内容被引用的次数（秒传命中会增加）。

---

### DELETE /v1/files/{file_id} — 删除文件

**请求：**
```
DELETE /v1/files/f1e2d3c40000000000000001
X-Namespace: my-ns
```

**响应 `204 No Content`：** 文件标记删除，引用计数 -1。计数归零时物理文件被清理。

---

## 批量管理 API（v0.1.0+）

### POST /v1/batch/{action} — 通用批量操作端点

> **v0.1.0+**：单个路径参数 `action` 区分批量操作类型，
> 替代 v0.0.x 的多端点设计。

支持的 `action` 值：

| action | 请求体 | 响应 |
|---|---|---|
| `delete` | `{"ids": [...]}` | `{"success": N, "failed": M}` |
| `move` | `{"ids": [...], "target_dir_id": "..."}` | `{"status": "ok"}` |
| `copy` | `{"ids": [...], "target_dir_id": "..."}` | `{"success": N, "failed": M}` |
| `tags` | `{"ids": [...], "tags": [...]}` | `{"status": "ok"}` |

**示例（批量复制）：**

```bash
curl -X POST http://localhost:8080/v1/batch/copy \
  -H "Content-Type: application/json" \
  -H "X-Namespace: default" \
  -d '{"ids":["f1","f2"],"target_dir_id":"d_target"}'
```

**响应 `200 OK`：**
```json
{
  "status": "ok",
  "success": 2,
  "failed": 0
}
```

**SDK 调用：**
```typescript
// JS SDK（v0.1.0+）
const result = await client.batchCopy(["id1", "id2"], "target-dir")
console.log(`复制 ${result.success} 成功，${result.failed} 失败`)
```
```go
// Go SDK（v0.1.0+）
res, err := client.BatchCopy(ctx, []string{"id1", "id2"}, "target-dir")
if err == nil {
    fmt.Printf("复制 %d 成功，%d 失败\n", res.Success, res.Failed)
}
```

---

### POST /v1/batch/copy — 批量复制（详细）

**请求：**
```json
{
  "ids": ["f1e2d3c40000000000000001", "f1e2d3c40000000000000002"],
  "target_dir_id": "f1e2d3c400000000000000a1"
}
```

**响应 `200 OK`（v0.1.0+）：**
```json
{
  "status": "ok",
  "success": 2,
  "failed": 0
}
```

`success` / `failed` 区分成功复制与跳过（找不到 / namespace 不匹配）的文件数。
v0.1.0 之前版本只返回 `{"status": "ok"}`。

**SDK 调用：**
```typescript
// JS SDK（v0.1.0+）
const result = await client.batchCopy(["id1", "id2"], "target-dir")
console.log(`复制成功 ${result.success} 个，失败 ${result.failed} 个`)
```
```go
// Go SDK（v0.1.0+）
res, err := client.BatchCopy(ctx, []string{"id1", "id2"}, "target-dir")
if err == nil {
    fmt.Printf("复制成功 %d 个，失败 %d 个\n", res.Success, res.Failed)
}
```

---

### DELETE /v1/dirs/{dir_id} — 删除目录

**请求：**
```
DELETE /v1/dirs/d_a1b2c3d40000000000000001
X-Namespace: my-ns
```

**响应 `204 No Content`：** 删除目录记录，不级联删除子文件。

---


## WebSocket

### GET /ws — 实时事件推送

WebSocket 端点，用于接收上传进度、完成、错误等实时事件。

**连接：**
```
ws://host:8080/ws
```

**服务端推送事件格式：**
```json
{
  "type": "upload_progress",
  "payload": {
    "task_id": "photo.jpg-1234567890-0.123",
    "file_name": "photo.jpg",
    "progress": 45,
    "speed": "5.2 MB/s",
    "status": "uploading"
  }
}
```

| 事件类型 | 说明 |
|---------|------|
| `upload_progress` | 上传进度更新 |
| `upload_complete` | 上传完成，含 `file_id` |
| `upload_error` | 上传失败，含 `error` 消息 |

---

### GET /health — 健康检查

**请求：**
```
GET /health
```

**响应 `200 OK`（v0.1.0+）：**
```json
{
  "status": "ok",
  "storage": {
    "status": "ok"
  },
  "metadata": {
    "status": "ok"
  }
}
```

`storage` / `metadata` 任一 `status: "error"` 表示对应后端不可达（PingContext / Stat 失败）。
v0.1.0 之前版本只返回 `{"status": "ok"}`。

---

## 鉴权 API

### POST /v1/auth/login — 用户登录

**请求：**
```
POST /v1/auth/login
Content-Type: application/json

{
  "username": "admin",
  "password": "admin123"
}
```

**响应 `200 OK`：**
```json
{
  "access_token": "eyJhbGciOiJIUzI1NiIs...",
  "refresh_token": "eyJhbGciOiJIUzI1NiIs...",
  "expires_in": 259200,
  "user_id": "u-admin",
  "namespace": "default"
}
```

**响应 `401 Unauthorized`：**
```json
{ "error": "认证失败", "code": "auth_failed" }
```

### POST /v1/auth/refresh — 刷新 Token

**请求：**
```
POST /v1/auth/refresh
Content-Type: application/json

{
  "refresh_token": "eyJhbGciOiJIUzI1NiIs..."
}
```

**响应 `200 OK`：** 返回新的 `access_token` 和 `refresh_token`。

### GET /v1/auth/me — 获取当前用户

**请求：**
```
GET /v1/auth/me
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...
```

**响应 `200 OK`：**
```json
{
  "user_id": "u-admin",
  "namespace": "default",
  "roles": ["admin", "user"]
}
```

---

## 预览 API

### GET /v1/preview/{file_id} — 在线预览文件

流式读取文件内容，返回正确的 Content-Type 实现浏览器内联预览。

**请求：**
```
GET /v1/preview/f1e2d3c40000000000000001
```

**支持的文件类型：**

| 类别 | 扩展名 | Content-Type |
|------|--------|-------------|
| 图片 | jpg, png, gif, webp, svg | image/* |
| 文档 | pdf | application/pdf |
| 文本 | txt, md, log, json, xml, csv, yaml | text/plain; charset=utf-8 |
| 代码 | go, js, py, ts, java, c, cpp, rs, sh | text/plain; charset=utf-8 |
| 视频 | mp4, webm, avi, mov, mkv | video/* |
| 音频 | mp3, wav, ogg, flac, aac | audio/* |

**响应：**
```
Content-Type: image/png
Content-Disposition: inline; filename="photo.png"
Cache-Control: public, max-age=3600

<binary data>
```

支持 Range 头分段读取，用于视频/音频拖拽播放。

---

## 分享链接 API

### POST /v1/share — 创建分享链接

**请求：**
```
POST /v1/share
Content-Type: application/json
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...

{
  "file_id": "f1e2d3c40000000000000001",
  "password": "optional-pass",
  "expires_in": 24,
  "max_downloads": 10
}
```

| 参数 | 必填 | 说明 |
|------|------|------|
| `file_id` | 是 | 要分享的文件 ID |
| `password` | 否 | 访问密码 |
| `expires_in` | 否 | 过期小时数（0=不限） |
| `max_downloads` | 否 | 最大下载次数（0=不限） |

**响应 `201 Created`：**
```json
{
  "token": "s-abc123def456...",
  "file_id": "f1e2d3c40000000000000001",
  "expires_at": "2026-06-23T22:00:00+08:00",
  "max_downloads": 10,
  "cur_downloads": 0,
  "namespace": "default"
}
```

### GET /s/{token} — 访问分享链接

**程序化请求：**
```
GET /s/s-abc123def456...
X-Share-Password: optional-pass
```

**浏览器访问：** 若链接受密码保护，首次 `GET` 会返回密码页面；通过 `POST /s/{token}` 提交
`application/x-www-form-urlencoded` 的 `password` 字段后，服务签发仅作用于该分享路径、有效期 15 分钟的
HttpOnly cookie，并以 `303 See Other` 跳回下载地址。首次展示密码页不会计入下载次数，也不会触发 bcrypt 密码比较。

**响应 `302 Found`：** 重定向到文件下载。

**错误：**
| 状态码 | 说明 |
|--------|------|
| 404 | 链接不存在或已过期 |
| 401 | 密码错误（程序化请求返回 `{"code": "share_password_required"}`；浏览器返回密码页） |
| 410 | 下载次数已达上限或已过期 |
| 429 | 同一客户端对同一分享链接连续 5 次密码验证失败后触发，冷却 15 分钟；响应含 `Retry-After`，程序化请求返回 `{"code": "share_password_rate_limited", "retry_after_seconds": ...}` |

---


## 管理 API

### GET /v1/admin/status — 系统状态

**请求：**
```
GET /v1/admin/status
```

**响应 `200 OK`：**
```json
{
  "version": "dev",
  "storage": {
    "data_dir": "/data",
    "temp_dir": "/tmp",
    "total_files": 100,
    "total_blobs": 50,
    "total_size": 104857600
  },
  "database": {
    "type": "sqlite",
    "path": "/data/fileupload.db"
  },
  "worker_pool": {
    "capacity": 8,
    "available": 6
  }
}
```

**SDK 调用：**
```typescript
// JS SDK
const status = await client.systemStatus()
console.log(status.storage.total_files, status.worker_pool.capacity)
```
```go
// Go SDK
status, err := client.GetSystemStatus(ctx)
if err == nil {
    fmt.Println(status.Counts["files"], status.Storage)
}
```

---

### GET /v1/admin/audit — 审计日志

**请求：**
```
GET /v1/admin/audit?page=1&per_page=50
```

**响应 `200 OK`：**
```json
{
  "entries": [
    {
      "id": 1, "action": "delete", "user_id": "u-admin",
      "detail": "批量删除 3 个文件", "created_at": "2026-06-22T12:00:00Z"
    }
  ],
  "total": 1, "page": 1, "per_page": 50
}
```

### POST /v1/admin/scan — 触发一致性巡检

**请求：**
```
POST /v1/admin/scan
```

**响应 `200 OK`：**
```json
{
  "orphan_parts": 2,
  "orphan_files": ["default/orphan-file-id"],
  "metadata_orphans": 0,
  "ref_count_fixes": 1,
  "corrupted_files": []
}
```

| 字段 | 说明 |
|------|------|
| `orphan_parts` | 临时目录中无对应会话的孤立文件 |
| `orphan_files` | 数据目录中无元数据记录的物理文件 |
| `metadata_orphans` | 元数据记录了但物理文件不存在的 blob |
| `ref_count_fixes` | 引用计数与实际引用文件数不一致的 blob 数量 |
| `corrupted_files` | SHA-256 校验失败的文件 |

---

## 请求头参考

| 头 | 适用 | 必填 | 说明 |
|------|------|------|------|
| `X-Namespace` | 全部 | 否 | 多租户隔离标识，缺省 `default` |
| `Authorization` | 全部 | 否 | 预留：Bearer token |
| `X-Request-ID` | 全部 | 否 | 请求追踪 ID，缺省自动生成 |
| `X-SHA256` | 上传创建 | 否 | 文件原始内容 SHA-256 |
| `X-Compression` | 上传创建 | 否 | `none` / `zstd` |
| `X-File-Name` | 上传创建 | 否 | 文件名（URL 编码） |
| `X-Chunk-Size` | tus POST | 否 | 分片大小（字节） |
| `X-Slice-Index` | 分片上传 | 否 | 分片序号 |
| `X-Slice-SHA256` | 分片上传 | 否 | 分片内容的 SHA-256 |
| `Upload-Length` | tus POST | 是 | 文件总大小（字节） |
| `Upload-Offset` | tus PATCH | 是 | 当前偏移量 |
| `Range` | 下载 | 否 | `bytes=start-end` |

---

## 速率限制

- 默认每个 namespace 或源 IP 每秒 100 请求，burst 200
- 超出返回 `429 Too Many Requests`，含 `Retry-After` 头
- 配置项（当前硬编码，后续可配置）：`rate=100, burst=200`
