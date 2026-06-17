# 文件上传下载服务（fileupload）设计文档

- **日期**：2026-06-17
- **状态**：设计已确认，待实现规划
- **范围**：首批交付「服务端 + Go CLI」；Web UI 与多语言 SDK 为后续迭代

---

## 0. 概述

设计一个 Go 语言文件上传下载服务，支持文件/目录上传下载、动态分片、客户端压缩/服务端解压、流式打包下载、SHA-256 校验、分片级并发、断点续传、秒传等业界常见功能。

### 0.1 已确认决策一览

| 决策项 | 选择 | 备注 |
|--------|------|------|
| 技术栈 | Go | — |
| HTTP 框架 | net/http + chi | 轻量、依赖少 |
| 存储后端 | 本地文件系统起步（可插拔 S3） | 端口抽象 |
| 上传协议 | tus + REST 双协议 | 共享 UploadService 核心 |
| 鉴权 | 无鉴权（网关代理） | 上游 Gateway 注入用户身份头 |
| 规模 | 中等规模 1–10TB，单实例为主 | 需限流/性能调优 |
| 分片粒度 | 动态分片大小 | tus 协议支持 |
| 压缩 | 上传客户端压缩 → 传输到存储时服务端解压 → 下载时服务端打包 | 目录+文件场景 |
| 数据校验 | 分片 + 整体 SHA-256 | 防篡改/防损坏 |
| 目录语义 | 递归上传 + 流式打包下载 | 打包成 .tar.gz/.zip |
| 并发控制 | 分片级并发 + worker 池 | 同一文件多分片并发 |
| 元数据 | Redis 热数据 + DB 冷数据 | 热数据=进行中会话/分片状态 |
| 冷数据 DB | SQLite + PG 可插拔 | 端口抽象，配置切换 |
| 客户端 | Go CLI（首批）+ Web UI（后续）+ 多语言 SDK（后续） | — |
| 范围优先级 | 服务端 + CLI 先行 | Web UI/SDK 后续迭代 |
| 断点续传 | 支持 | tus 原生，REST 自实现 |
| 秒传 | 支持（按内容哈希去重） | 客户端先查 SHA-256 |
| 生命周期 | 上传会话超时清理 + 仅手动删除 + 一致性巡检 | 无自动过期 |
| 目录下载打包 | 流式打包下载 | 边压缩边发送 |
| 下载 Range | 支持 HTTP Range（纳入首批） | 分段下载/续传 |
| 架构方案 | 方案 A：分层 + 共享 UploadService 核心 | 两协议收敛到同一领域核心 |

---

## 1. 系统架构与分层

采用**方案 A：分层 + 共享 UploadService 核心**。两条协议入口（tus handler、REST handler）都收敛到一个共享的领域核心，核心再通过端口/适配器对接底层能力。

```
┌─────────────────────────────────────────────────────────────────┐
│                       客户端                                    │
│   Go CLI / Web UI / 多语言 SDK（后续）                          │
└──────────────────────────┬──────────────────────────────────────┘
                           │ HTTP（tus 协议 + REST）
┌──────────────────────────▼──────────────────────────────────────┐
│  传输层 Transport（net/http + chi）                             │
│  ┌──────────────────┐  ┌──────────────────┐  ┌──────────────┐  │
│  │ tus Handler      │  │ REST Handler     │  │ 下载 Handler │  │
│  │（POST/PATCH/HEAD│  │（init/chunk/merge│  │（流式打包    │  │
│  │   tus 标准）     │  │   自定义接口）    │  │  下载）       │  │
│  └────────┬─────────┘  └────────┬─────────┘  └──────┬───────┘  │
│           └──────────┬──────────┘                    │          │
└──────────────────────┼──────────────────────────────┼──────────┘
                       ▼                              │
┌───────────────────────────────────────────────────┐ │
│  领域核心 Domain Core                              │ │
│  ┌─────────────────────────────────────────────┐  │ │
│  │ UploadService（上传编排）                   │──┘ │
│  │   会话生命周期 / 秒传判断 / 分片并发 / 合并  │    │
│  │   上传后解压 / SHA-256 整体校验              │    │
│  └─────────────────────────────────────────────┘    │
│  ┌─────────────────────────────────────────────┐    │
│  │ DownloadService（下载编排）                  │◄───┘
│  │   目录流式打包 / 单文件直传 / 返回校验和      │
│  └─────────────────────────────────────────────┘    │
└──────────────────────────┬──────────────────────────┘
                           ▼（端口/接口）
┌──────────────────────────────────────────────────────────────────┐
│  基础设施适配层 Adapters（可插拔）                                │
│  ┌──────────────┐ ┌──────────────────┐ ┌──────────┐ ┌─────────┐ │
│  │ Storage 端口 │ │ Metadata 端口    │ │Compressor│ │ Hasher  │ │
│  │  → 本地 FS   │ │ → Redis热+DB冷   │ │ gzip/zstd│ │SHA-256  │ │
│  │ （未来→S3）  │ │   DB:SQLite/PG   │ │ tar/zip  │ │         │ │
│  └──────────────┘ └──────────────────┘ └──────────┘ └─────────┘ │
└──────────────────────────────────────────────────────────────────┘
```

### 三层职责

- **传输层**：只负责 HTTP 协议解析、路由、限流、把请求翻译成对核心的调用。不含业务逻辑。tus Handler 实现 tus.io 协议（`POST` 创建、`PATCH` 追加分片、`HEAD` 查询偏移）；REST Handler 实现自定义分片接口；下载 Handler 负责流式打包响应。
- **领域核心**：业务大脑。`UploadService` 编排上传会话、秒传、并发分片合并、上传后解压、整体校验；`DownloadService` 编排打包下载与校验和返回。两协议共享同一核心——这是避免逻辑重复的关键。
- **适配层**：四个端口（`Storage` / `Metadata` / `Compressor` / `Hasher`）各定义接口，具体实现可插拔，满足「本地可插 S3」「SQLite+PG 可插拔」「Redis 热数据 + DB 冷数据」。

**设计原则**：每层一个职责，层间只通过定义好的接口通信，可独立测试。CLI 直接走 HTTP API 调传输层，不重复实现业务。

---

## 2. 上传数据流 + 元数据模型

### 2.1 上传完整数据流（以 tus 为主流程，REST 同构）

```
客户端                                    服务端
 │                                         │
 │  ① 预检秒传：HEAD /files?sha256=xxx      │
 │ ───────────────────────────────────────▶│ UploadService.CheckExists(sha256)
 │                                         │  → 查 DB 冷数据（按内容哈希）
 │ ◀── 命中：直接复用，返回 fileID（秒传）  │
 │                                         │
 │  ② 未命中：POST /uploads (tus 创建)      │
 │     Header: Upload-Length, X-SHA256,    │
 │     X-Compression=zstd, X-Chunk-Size    │
 │ ───────────────────────────────────────▶│ 创建 UploadSession（写 Redis 热数据）
 │                                         │  分配临时目录 tmp/<sessionID>/
 │ ◀── 201 Location: /uploads/<sessionID>  │
 │                                         │
 │  ③ 并发 PATCH 分片（N 个 worker）        │
 │     PATCH /uploads/<id> (offset=…)      │
 │ ───────────────────────────────────────▶│ 分片落入 worker 池
 │     带 X-Slice-SHA256 分片校验和         │  → 校验分片 SHA-256 → 落盘 tmp/<id>/<n>
 │ ◀── 204 + Upload-Offset                 │  → 更新 Redis 分片状态
 │                                         │
 │  ④ 全部分片到位 → UploadService.Finalize │
 │                                         │  按分片 index 顺序读取 tmp → 合并
 │                                         │  边合并边算整体 SHA-256
 │                                         │  比对客户端声明的 X-SHA256
 │                                         │  若 X-Compression=zstd → 解压还原原始数据
 │                                         │  写入 Storage 最终路径 data/<namespace>/<fileID>
 │                                         │  会话信息从 Redis 迁移到 DB 冷数据
 │ ◀── 200 { fileID, sha256, size }        │
```

### 2.2 元数据模型

**Redis 热数据**（进行中的上传会话 + 分片状态，短生命周期）

```
upload:session:<sessionID>          Hash {
  fileID?, sha256(声明), uploadLength,
  compression(zstd|none), chunkSize,
  namespace(用户身份头), createdAt, expireAt,
  status: active|finalizing|completed|aborted
}
upload:chunks:<sessionID>           Hash { <index>: <sliceSha256>, … }   // 已落盘分片索引
upload:offset:<sessionID>           // 当前已接收字节偏移（tus Upload-Offset）
```

**DB 冷数据**（已完成的文件信息，持久化）

```sql
-- 内容寻址去重表：sha256 → 物理文件（秒传核心）
content_blobs(
  sha256       TEXT PRIMARY KEY,
  storage_path TEXT NOT NULL,     -- data/<namespace>/<fileID>
  size         BIGINT NOT NULL,
  ref_count    INT NOT NULL,      -- 引用计数，支持去重删除
  created_at   TIMESTAMP
)

-- 文件节点表（逻辑文件/目录项，可多个逻辑名指向同一 blob）
files(
  fileID       TEXT PRIMARY KEY,
  sha256       TEXT REFERENCES content_blobs(sha256),
  name         TEXT NOT NULL,
  path         TEXT NOT NULL,     -- 逻辑路径（含目录相对路径）
  size         BIGINT,
  namespace    TEXT,
  is_dir       BOOLEAN,
  parent_id    TEXT,              -- 目录树（递归上传用）
  created_at   TIMESTAMP
)
```

### 2.3 目录上传的处理

目录 = manifest 上传：客户端递归遍历目录，对每个文件走 2.1 的单文件流程，再 `POST /dirs` 提交一个 manifest（文件相对路径列表 + 各自 fileID）。服务端在 `files` 表里建出目录树（`parent_id` 关联），根目录返回一个 `dirID`。下载时按 `dirID` 递归取子节点，流式打包。

### 2.4 命名空间隔离

「网关注入用户身份头」：传输层从请求头（如 `X-User-ID`）读 namespace，注入到 session 的 `namespace` 字段，写入时落到 `data/<namespace>/<fileID>`、DB 查询按 namespace 过滤。多租户隔离，无需服务端自建鉴权。

---

## 3. 下载数据流 + 流式打包

### 3.1 单文件下载

```
客户端                                      服务端
 │  GET /files/<fileID>?namespace=…         │
 │ ────────────────────────────────────────▶│ DownloadService.GetFile(fileID, ns)
 │                                           │  → 查 DB files 表校验 namespace
 │                                           │  → 打开 Storage data/<ns>/<fileID>
 │                                           │  → 边读边算 SHA-256（与存储的 sha256 比对，可选校验）
 │ ◀── 200 Content-Type, Content-Length,    │
 │      X-SHA256: <hex>, 流式 body           │
```

- 响应头带 `X-SHA256`，客户端下载后可自行校验（与上传校验对称）。
- 支持 `Range` 请求（HTTP 标准分段下载），便于客户端断点续传下载。

### 3.2 目录下载（流式打包）

```
GET /dirs/<dirID>?format=tar.gz&namespace=…
    │
    ▼
DownloadService.StreamDir(dirID, ns, format) → io.Reader
    1. 查 DB files 表，递归取 dirID 下所有子节点（BFS/DFS）
    2. 构造一个流式打包管道：
         文件遍历器 ──▶ tar.Writer ──▶ gzip/zstd.Writer ──▶ http.ResponseWriter
       （多级 io.Pipe 串联，边读文件边打包边发送，全程不落临时包文件）
    3. 边打包边算整体流校验和（可选）
    4. 响应头：
         Content-Type: application/gzip
         Content-Disposition: attachment; filename="<dir>.tar.gz"
         X-Tree-SHA256: <目录树 manifest 的哈希>   // 可选，目录级完整性
```

### 3.3 流式打包管道（核心数据结构）

```
                    http.ResponseWriter
                            ▲
                            │ 流式写入（Flush 逐块）
                   zstd/gzip Writer
                            ▲
                            │
                      tar Writer   ← 写入 header + 文件内容
                            ▲
                            │
                  DirWalker（DFS）
                   从 DB 取子节点 → 打开每个文件 Storage reader
                            ▲
                            │
                  Storage.Open(path)   （按需打开，惰性读取）
```

- 关键：`DirWalker` 用 `io.Pipe` 把「遍历→写 tar entry」做成生产者-消费者，避免一次性把目录树全读进内存。大目录也能流式。
- `format` 支持 `tar.gz` / `tar.zst` / `zip`；默认 `tar.gz`。
- 任意一环出错：关闭 pipe 写端返回错误，HTTP 已发头则中断连接（流式下载无法改状态码，客户端按字节数/校验和感知）。

### 3.4 目录 X-Tree-SHA256 计算

目录的 `X-Tree-SHA256` 需要在流开始**前**就知道，但 manifest 要遍历完才能得到。解决：遍历目录树时**先做一次轻量 DB 查询**拿到所有 (path, size, sha256)，算出 manifest 哈希写进响应头，再启动流式打包管道发送内容。代价是一次额外查询，换来下载即可校验完整性。

### 3.5 下载校验和策略

| 场景 | 校验和 |
|------|--------|
| 单文件 | `X-SHA256`（文件内容哈希，来自 content_blobs） |
| 目录 | `X-Tree-SHA256`（目录 manifest 的有序序列化哈希，含每文件 sha256 + 相对路径 + 大小） |
| 传输完整性 | 客户端下载后比对，不匹配则重试/续传（Range） |

---

## 4. 压缩/解压编排 + SHA-256 校验

### 4.1 压缩格式与职责划分

| 阶段 | 谁 | 做什么 | 格式 |
|------|----|--------|------|
| 上传前 | 客户端 | 单文件 `zstd`；目录先 `tar` 再 `zstd` 成单流 | zstd（单文件）/ tar.zst（目录） |
| 传输→存储 | 服务端 | Finalize 时解压，**存储原始数据** | 解压还原 |
| 下载 | 服务端 | 单文件直传原始；目录流式 `tar→zstd/gzip` 打包 | tar.gz / tar.zst / zip |

**核心约定**：存储层永远是原始未压缩数据。这样：①去重按原始内容 SHA-256，秒传语义清晰；②下载可按需选格式；③压缩只是传输/下载打包的临时优化，不污染存储。

### 4.2 上传解压编排（Finalize 阶段）

```
分片合并后的压缩流（tmp/<id>/merged.zst 或 merged.tar.zst）
        │
        ▼
   Compressor.Decompress(reader) → io.Reader（原始数据）
        │
        ├─▶ Hasher（SHA-256）边读边算 → 得到原始内容 sha256
        │
        ▼
   Storage.Write(data/<ns>/<fileID>, reader)
        │
        ▼
   比对客户端声明 X-SHA256（原始内容的哈希）
     ├─ 匹配 → 写 content_blobs（sha256, path, ref_count=1）+ files 记录
     └─ 不匹配 → 删除已写数据，返回校验失败错误（422）
```

关键约定：
- **客户端声明的 X-SHA256 指原始内容哈希**（解压后），不是压缩流哈希。这样秒传预检和存储去重都基于原始内容，一致。
- 分片级校验用 `X-Slice-SHA256`（压缩分片的哈希），保证传输过程中分片本身没坏；整体校验用解压后的原始 SHA-256。

### 4.3 下载打包编排

```
（单文件）Storage.Open → 直接流式发送（可选边发边算校验）
（目录）  DirWalker → tar.Writer → Compressor(gzip/zstd).Writer → Response
              ↑ 每写入一个文件 entry，同步记录 (path, size, sha256) 到 manifest
        → manifest 哈希在流开始前算好放响应头（见 3.4）
```

### 4.4 SHA-256 校验全景

| 校验点 | 对象 | 谁算 | 何时 | 失败处理 |
|--------|------|------|------|---------|
| 分片校验 | 压缩分片字节 | 服务端（落盘后） | PATCH 每个 chunk | 拒绝该分片，返回 409/460，客户端重传该片 |
| 整体校验 | 解压后原始内容 | 服务端（Finalize 边合并边算） | 合并+解压时 | 删除数据，422，整个上传失败重试 |
| 秒传匹配 | 原始内容 SHA-256 | 客户端上传前算 | 预检 HEAD | 命中则复用 content_blobs，ref_count+1 |
| 下载校验 | 文件内容 | 客户端下载后算 | 收完 body | 不匹配则重试/续传（Range） |

---

## 5. 并发 worker 池 + 断点续传 + 秒传

### 5.1 分片并发上传 + worker 池

**两层并发模型**：

```
全局层（服务端进程级）
  ChunkWorkerPool（固定大小，如 GOMAXPROCS）
  └─ 所有上传会话共享，避免单进程打爆磁盘 IO

会话层（每个 UploadSession）
  并发接收多个 PATCH（tus/REST 都支持）
  每个分片请求 → 提交任务到全局 worker 池
```

**单个分片处理流程**（在 worker 中执行）：

```
PATCH /uploads/<id> (offset, X-Slice-SHA256)
   │
   ▼
worker:
  1. 读取请求 body → 临时缓冲/直接流式写 tmp/<id>/<index>.part
  2. Hasher.Sum(part) → 比对 X-Slice-SHA256
       └─ 不匹配 → 删除 .part，返回分片校验失败，客户端重传该片
  3. CAS 更新 Redis：upload:offset:<id>（原子增，防并发覆盖）
       └─ 用 Lua 脚本保证 offset 连续性校验
  4. 记录 upload:chunks:<id> <index>=<sliceSha256>
  5. 返回 204 + 新 Upload-Offset
```

**并发安全要点**：
- 分片**乱序到达**也能落盘（按 index 存 `.part`，合并时按 index 排序）。
- `offset` 用 Redis Lua 原子更新，保证多分片并发时偏移量正确。
- worker 池满时，HTTP 请求排队等待（带超时），返回 503 提示客户端稍后重试。
- 配置上限：单会话最大并发分片数、worker 池大小、全局在途分片数。

### 5.2 断点续传

**tus 协议路径**（原生支持）：
- 客户端中断后，`HEAD /uploads/<id>` → 服务端返回 `Upload-Offset`（已接收字节数）。
- 客户端从 offset 继续 `PATCH`，跳过已传部分。

**REST 协议路径**（自实现）：
- `GET /uploads/<id>/status` → 返回已落盘分片索引列表 + 总进度。
- 客户端比对本地分片，只 `PUT /uploads/<id>/chunks/<index>` 缺失的分片。
- 合并触发：客户端确认全部分片到位后 `POST /uploads/<id>/finalize`。

**会话存活**：UploadSession 在 Redis 有 `expireAt`，续传期间靠活跃 PATCH 续期；超时未活跃 → 会话失效，临时分片进入清理（见第 7 节）。

### 5.3 秒传（内容寻址去重）

**预检流程**：
```
客户端上传前本地算原始内容 SHA-256
   │
   ▼
HEAD /files?sha256=<hex>&namespace=<ns>
   │
   ▼
UploadService.CheckExists(sha256, ns):
  1. 查 content_blobs WHERE sha256=?
     ├─ 不存在 → 返回 404，走正常上传
     └─ 存在 → ref_count+1，在 files 表新建逻辑文件记录
              （指向同一 blob，但有自己的 fileID/name/path/namespace）
   │
   ▼
返回 200 { fileID, sha256, size }   ← 客户端无需传输任何字节
```

**秒传 + 目录组合**：目录 manifest 提交时，每个文件条目可带「已秒传的 fileID」直接引用，不必先 HEAD 再传。

**去重删除安全**：
- 删除逻辑文件 → `ref_count-1`。
- `ref_count=0` 才真正删除 `content_blobs` 记录 + Storage 物理文件。
- 一致性巡检兜底（第 7 节）防止计数漂移。

---

## 6. 端口接口定义

### 6.1 Storage 端口（存储后端，本地 FS 起步，可插 S3）

```go
type Storage interface {
    // 写入：从 reader 流式写到 path，返回写入字节数
    Write(ctx, path string, r io.Reader) (n int64, err error)
    // 打开读取：返回可 Read + Close 的句柄，支持 Range（offset/length，0表示到尾）
    Open(ctx, path string, offset, length int64) (io.ReadCloser, error)
    // 删除物理文件
    Delete(ctx, path string) error
    // 文件信息（大小、是否存在）
    Stat(ctx, path string) (size int64, exists bool, err error)
}
```
- 本地实现：`LocalFSStorage`，path = `data/<namespace>/<fileID>`。
- 未来 S3 实现：`S3Storage`，path 映射为 object key，Open 用 GetObject 的 Range。

### 6.2 Metadata 端口（Redis 热 + DB 冷，门面模式）

```go
type Metadata interface {
    // === 热数据：上传会话（Redis）===
    CreateSession(ctx, s *UploadSession) error
    GetSession(ctx, id string) (*UploadSession, error)
    UpdateOffset(ctx, id string, sliceIndex int, sliceSha string, addBytes int64) error  // Lua 原子
    ListChunks(ctx, id string) ([]ChunkInfo, error)
    DeleteSession(ctx, id string) error
    TouchSession(ctx, id string, ttl time.Duration) error  // 续期

    // === 冷数据：已完成文件（DB）===
    // 内容寻址去重
    GetBlobBySha(ctx, sha256 string) (*ContentBlob, error)
    PutBlob(ctx, b *ContentBlob) error
    IncrBlobRef(ctx, sha256 string) error
    DecrBlobRef(ctx, sha256 string) (newCount int, err error)
    // 文件节点（逻辑文件 + 目录树）
    PutFile(ctx, f *File) error
    GetFile(ctx, id string) (*File, error)
    ListChildren(ctx, parentID string) ([]*File, error)   // 目录遍历用
    DeleteFile(ctx, id string) error
}
```
- 门面内部：会话相关路由到 `RedisStore`，blob/file 路由到 `DBStore`（SQLite/PG 可插拔，通过 `DBStore` 子接口实现）。

### 6.3 Compressor 端口（压缩/解压/打包）

```go
type Format int
const (
    FormatZstd Format = iota
    FormatGzip
    FormatTarZst
    FormatTarGz
    FormatZip
)

type Compressor interface {
    // 上传 Finalize 解压：输入压缩流，输出原始数据流
    Decompress(ctx, r io.Reader, fmt Format) (io.ReadCloser, error)
    // 下载打包：返回可流式写入的归档器
    NewArchiveWriter(ctx, w io.Writer, fmt Format) (ArchiveWriter, error)
}

type ArchiveWriter interface {
    // 写入一个文件条目（路径+内容），可多次调用
    AddFile(ctx, name string, size int64, content io.Reader) error
    // 收尾（写 footer/flush），完成后 w 可 Flush 到 HTTP
    Close() error
}
```

### 6.4 Hasher 端口（SHA-256）

```go
type Hasher interface {
    // 边读边算，返回哈希(hex) + 读取字节数；读完后 reader 已耗尽
    Sum(ctx, r io.Reader) (sha256hex string, n int64, err error)
    // 流式 Tee：返回一个 reader，读它的同时算哈希，最后取 Sum()
    TeeReader(r io.Reader) (io.Reader, *HashAccumulator)
}
```

### 6.5 端口装配与依赖注入

```
main 启动时按配置组装：
  storage  := NewLocalFSStorage(cfg.DataDir)         // 或 NewS3Storage(cfg)
  db       := NewSQLiteStore(cfg.DBPath)              // 或 NewPGStore(cfg)
  redis    := NewRedisStore(cfg.RedisAddr)
  meta     := NewMetadataFacade(redis, db)
  compress := NewZstdCompressor()                     // 内置 zstd/gzip/tar
  hasher   := NewSHA256Hasher()

  uploadSvc   := NewUploadService(meta, storage, compress, hasher, workerPool)
  downloadSvc := NewDownloadService(meta, storage, compress, hasher)

  传输层注入 uploadSvc/downloadSvc → 注册 tus + REST + 下载路由
```

- 所有接口在 `internal/ports` 定义，实现在 `internal/adapters/*`。
- 领域核心只依赖接口，不依赖任何具体实现 → 单元测试可用 mock 替换全部依赖。
- 接口当前覆盖已确认能力，后续遇到不足再按需补充。

---

## 7. 生命周期 + 错误处理

### 7.1 上传会话超时清理

**触发方式**：后台 goroutine 定时扫描（如每 1 分钟）。

```
SessionReaper（定时任务）
  1. 扫描 Redis：upload:session:* WHERE expireAt < now OR status=aborted
  2. 对每个过期会话：
       a. 删除 tmp/<sessionID>/ 下所有 .part 临时分片
       b. 删除 Redis：upload:session:<id> / upload:chunks:<id> / upload:offset:<id>
  3. 记录清理日志（会话ID、清理字节数）
```

- TTL 续期：每次活跃 PATCH 调 `TouchSession` 把 `expireAt` 后推（默认 1 小时无活动即过期）。
- 防止清理与 Finalize 竞态：Finalize 前先把 status 置 `finalizing` 并续期，reaper 跳过 `finalizing` 状态。

### 7.2 一致性巡检（ConsistencyScanner）

**触发方式**：低频定时（如每天 1 次）或 CLI 手动触发 `fileupload scan`。

| 检查项 | 检测 | 修复 |
|--------|------|------|
| 孤儿临时分片 | `tmp/` 下有分片但对应 session 在 Redis 已不存在 | 删除临时分片 |
| 孤儿物理文件 | `data/` 下文件在 DB content_blobs 无记录 | 隔离到 `quarantine/` 待人工确认 |
| 元数据孤儿 | DB content_blobs 有记录但 Storage 文件丢失 | 标记 blob 损坏，相关 files 标记不可用 |
| 引用计数漂移 | content_blobs.ref_count ≠ files 表实际引用数 | 重新统计修正 ref_count |
| 文件大小/哈希不符 | Storage.Stat 的 size ≠ DB 记录 | 标记损坏，记入巡检报告 |

- 巡检产出报告（JSON/日志），**默认只报告 + 安全修复**（删孤儿临时分片、修 ref_count）；涉及删除用户数据的（孤儿物理文件）只隔离不删，需人工确认。
- 用锁防止多实例并发巡检冲突（单实例阶段用进程内锁即可）。

### 7.3 删除语义（仅手动）

```
DELETE /files/<fileID>   或   fileupload rm <fileID>
   │
   ▼
UploadService.Delete(fileID):
  1. 查 DB files 表，校验 namespace
  2. 删 files 记录
  3. DecrBlobRef(sha256)
     └─ newCount==0 → 删 content_blobs 记录 + Storage.Delete(物理文件)
     └─ newCount>0  → 物理文件保留（其他逻辑文件还在引用）
  4. 目录删除：递归删所有子节点 files 记录，每个叶子走上面流程
```

### 7.4 错误处理与错误码

**统一错误模型**：领域核心返回领域错误（typed error），传输层映射为 HTTP 状态码 + JSON。

| 场景 | 领域错误 | HTTP | 客户端动作 |
|------|---------|------|-----------|
| 分片 SHA-256 不匹配 | `ErrSliceChecksum` | 460 (tus) / 409 | 重传该分片 |
| 整体 SHA-256 不匹配 | `ErrContentChecksum` | 422 | 整文件重传 |
| 会话不存在/已过期 | `ErrSessionNotFound` | 404 | 重新发起上传 |
| 会话已 finalizing/完成 | `ErrSessionState` | 409 | 轮询结果 |
| offset 冲突（分片重叠） | `ErrOffsetConflict` | 460/409 | 按 HEAD 重传 |
| 秒传未命中 | （非错误） | 404 | 走正常上传 |
| namespace 不匹配 | `ErrForbidden` | 403 | — |
| worker 池满/限流 | `ErrBusy` | 503 + Retry-After | 退避重试 |
| 存储写失败 | `ErrStorage` | 500 | 重试 |
| 巡检标记损坏的文件下载 | `ErrCorrupted` | 410 Gone | 联系管理员 |

**通用机制**：
- 传输层中间件统一 `recover` panic → 500 + 结构化日志。
- 所有错误带 `requestID`（请求头注入），日志可串联追踪。
- 可恢复错误（限流、存储瞬断）客户端用指数退避重试；不可恢复（校验失败）需人工介入或重传。

---

## 8. 测试策略 + CLI 设计

### 8.1 测试策略（TDD，三层覆盖）

**① 单元测试 — 端口与核心**
- 四个端口适配器各有独立测试：用真实本地 FS / 嵌入式 Redis（miniredis）/ SQLite 内存库，验证 Storage/Metadata/Compressor/Hasher 行为。
- `UploadService` / `DownloadService` 用 mock 端口测试编排逻辑（会话生命周期、Finalize 合并+解压+校验、秒传命中/未命中、并发分片 offset 原子性、去重引用计数）。
- 错误注入：mock 返回 `ErrStorage`/`ErrSliceChecksum`，验证核心的失败路径与状态回滚。

**② 集成测试 — 端到端**
- 起真实服务（内存 Redis + SQLite 临时库 + 临时数据目录），用真实 HTTP 客户端跑完整流程：
  - 单文件 tus 上传 → 下载 → 校验 SHA-256
  - 大文件分片 + 并发 + 断点续传（中途 kill，从 offset 继续）
  - 秒传：相同内容第二次上传命中
  - 目录递归上传 → 流式打包下载 → 解包比对
  - 客户端压缩上传 → 服务端解压 → 下载原始
  - 删除去重：两个逻辑文件共享 blob，删一个物理文件仍在
  - 错误场景：分片校验失败、整体校验失败、namespace 越权

**③ 并发与压力测试**
- `go test -race` 跑全部测试，确保无数据竞争。
- 并发上传同一文件多分片、并发上传不同文件、worker 池满限流（503）。
- 中等规模压测脚本：N 并发 × M 大小，测吞吐与延迟（CLI 提供 `fileupload bench`）。

**④ 巡检/清理测试**
- 构造孤儿临时分片、ref_count 漂移、元数据孤儿，验证 ConsistencyScanner 检测 + 安全修复 + 隔离行为。
- 会话超时：构造过期 session，验证 reaper 清理。

### 8.2 Go CLI 设计

**命令结构**（`fileupload` 主命令 + 子命令，走 HTTP API 调服务端）：

```
fileupload upload <path>              # 上传文件或目录（自动递归）
  --chunk-size 10m                    # 动态分片大小（默认按文件大小自适应）
  --concurrency 4                     # 并发分片数
  --compress zstd                     # 客户端压缩（默认 zstd）
  --resume                            # 断点续传（默认开）
  --server https://up.example.com
  --namespace <ns>                    # 或从配置/环境读

fileupload download <fileID|dirID> [-o out] [--format tar.gz]
  --range <start-end>                 # 分段下载/续传
  --verify                            # 下载后校验 SHA-256

fileupload rm <fileID|dirID>          # 删除（目录递归）

fileupload ls <dirID|/>               # 列目录树

fileupload stat <fileID>              # 查文件信息（sha256/size/引用数）

fileupload scan                       # 手动触发一致性巡检（管理员）

fileupload bench                      # 压测（并发×大小，测吞吐）

fileupload config                     # 查看/设置默认 server/namespace
```

**CLI 内部模块**（复用服务端同款端口抽象，保证客户端与服务端逻辑一致）：
```
cmd/fileupload/
  upload.go      # 上传编排：算原始SHA→秒传预检→压缩→分片→并发PATCH→finalize
  download.go    # 下载：目录流式收包解压 / 单文件 Range 续传 + 校验
  progress.go    # 进度条（多分片并发进度）
  compress.go    # 客户端压缩（zstd / tar.zst）
  checksum.go    # 本地算 SHA-256（秒传预检用）
  client.go      # HTTP 客户端（tus + REST 封装）
```

- CLI 与服务端共享 `pkg/` 里的压缩/哈希/分片逻辑（同 module，避免重复实现）→ 这也是方案 A 为后续 SDK 复用铺路的体现。
- 进度条：并发分片用多段进度条显示，断点续传时显示已传/待传。

### 8.3 项目目录结构

```
fileupload/
├── cmd/
│   ├── server/          # 服务端入口 main
│   └── fileupload/      # CLI 入口 main
├── internal/
│   ├── ports/           # Storage/Metadata/Compressor/Hasher 接口定义
│   ├── adapters/        # 具体实现（localfs/redis/sqlite/pg/zstd/sha256）
│   ├── domain/          # UploadService / DownloadService 核心
│   ├── transport/       # tus handler / REST handler / 下载 handler / 中间件
│   ├── lifecycle/       # SessionReaper / ConsistencyScanner
│   └── config/          # 配置加载
├── pkg/                 # CLI 与服务端共享的客户端库（压缩/哈希/分片/tus client）
├── docs/
│   └── superpowers/specs/2026-06-17-fileupload-design.md
├── task_plan.md / findings.md / progress.md
├── go.mod
└── README.md
```

---

## 9. 后续迭代（不在首批范围）

- **Web UI**：拖拽上传、目录树浏览、下载、进度展示。
- **多语言 SDK**：基于共享核心库，通过 gRPC 网关或 FFI 暴露给 Python/JS 等。
- **S3 存储适配器**：实现 `Storage` 端口的 S3 版本，配置切换。
- **PostgreSQL 冷数据**：实现 `DBStore` 的 PG 版本，为多节点扩展铺路。
- **多节点/水平扩展**：分布式 worker 池、跨节点分片。

---

## 10. 开放问题（实现阶段确认）

1. **客户端压缩的具体流式实现**：大文件压缩时是否需要分片压缩（独立解压每个分片）以支持分片级解压校验，还是整体压缩后切片。首批建议：整体压缩后切片（实现简单），分片级压缩作为后续优化。
2. **Redis 持久化配置**：AOF 还是 RDB，取决于对会话数据丢失的容忍度。建议 AOF（每秒 fsync）。
3. **namespace 头的可信来源**：网关注入后，服务端是否需要额外校验头来源 IP/签名。首批假设网关可信，后续按部署环境补充。
