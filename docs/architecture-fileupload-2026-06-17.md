# System Architecture: fileupload

**Document Version:** 1.0
**Date:** 2026-06-17
**Author:** System Architect
**Status:** Approved

**Source Requirements:** `docs/superpowers/specs/2026-06-17-fileupload-design.md`

---

## Table of Contents

1. [System Overview](#1-system-overview)
2. [Architecture Pattern](#2-architecture-pattern)
3. [Component Design](#3-component-design)
4. [Data Model](#4-data-model)
5. [API Specifications](#5-api-specifications)
6. [Non-Functional Requirements Mapping](#6-non-functional-requirements-mapping)
7. [Technology Stack](#7-technology-stack)
8. [Trade-off Analysis](#8-trade-off-analysis)
9. [Deployment Architecture](#9-deployment-architecture)
10. [Future Considerations](#10-future-considerations)

---

## 1. System Overview

### Purpose

fileupload 是一个用 Go 实现的文件上传下载服务，支持文件与目录的上传下载、动态分片、客户端压缩/服务端解压、流式打包下载、SHA-256 数据校验、分片级并发、断点续传、秒传（内容寻址去重）等业界常见功能。首批交付「服务端 + Go CLI」，后续迭代 Web UI 与多语言 SDK。

### Scope

**In Scope (首批):**
- 单文件上传（tus 协议 + 自定义 REST 双协议）
- 目录递归上传（manifest 方式）
- 分片级并发上传 + worker 池
- 客户端 zstd 压缩 → 服务端 Finalize 解压 → 存储原始数据
- 断点续传（tus 原生 + REST 自实现）
- 秒传（按原始内容 SHA-256 去重，content_blobs 引用计数）
- 单文件下载（带 X-SHA256 + HTTP Range）
- 目录流式打包下载（tar.gz / tar.zst / zip，io.Pipe 管道）
- SHA-256 分片级 + 整体级校验
- 上传会话超时清理、一致性巡检、手动删除
- Go CLI（upload/download/rm/ls/stat/scan/bench/config）
- 本地文件系统存储 + SQLite 冷数据 + Redis 热数据

**Out of Scope (后续迭代):**
- Web UI、多语言 SDK
- S3 存储适配器（端口已预留）
- PostgreSQL 冷数据适配器（端口已预留）
- 多节点/水平扩展、分布式 worker 池

### Architectural Drivers

影响架构的关键需求（NFR）：

1. **ADR-1: 中等规模吞吐（1–10TB，数百级并发连接）** — 决定了 Go + 分片级并发 + worker 池 + 流式 IO 的核心选择；避免一次性加载大文件进内存。
2. **ADR-2: 数据完整性（防篡改/防损坏）** — 决定了四点 SHA-256 校验体系（分片/整体/秒传/下载）贯穿全流程。
3. **ADR-3: 可插拔基础设施（存储/DB 可替换）** — 决定了端口/适配器分层，领域核心不依赖具体实现。
4. **ADR-4: 双协议共存避免逻辑重复** — 决定了 tus + REST 共享同一 UploadService 核心。
5. **ADR-5: 秒传去重的存储经济性** — 决定了内容寻址（content_blobs）+ 引用计数模型，存储层只存原始数据。

### Stakeholders

- **Users:** 通过 CLI 或后续 SDK/Web UI 上传下载文件/目录的开发者与业务系统。
- **Developers:** 单人/小团队，Go 技术栈。
- **Operations:** 单实例部署起步，由上游网关负责鉴权与流量入口。
- **Business:** casdao 项目下的文件存储基础设施。

---

## 2. Architecture Pattern

### Selected Pattern

**Pattern:** 分层架构（Layered）+ 端口/适配器（Hexagonal）+ 共享领域核心

具体三层：
- **传输层 Transport**（net/http + chi）
- **领域核心 Domain Core**（UploadService / DownloadService）
- **适配层 Adapters**（Storage / Metadata / Compressor / Hasher 端口实现）

### Pattern Justification

**Why this pattern:**
- 中等规模单实例，不需要微服务的运维复杂度，分层单进程最简。
- 双协议（tus + REST）需共享业务逻辑，领域核心层是收敛点，避免逻辑重复。
- 存储/DB 需可插拔（本地→S3，SQLite→PG），端口/适配器让领域核心不感知基础设施。
- CLI 与服务端共享压缩/哈希/分片逻辑，同 module 内 `pkg/` 复用，为后续 SDK 铺路。

**Alternatives considered:**
- **协议并行双管道：** tus 与 REST 各自独立实现上传流程。拒绝——校验/秒传/合并逻辑会写两份，长期维护漂移风险高。
- **库优先（核心包先行）：** 先做纯库再套薄壳。拒绝——起步接口设计过重，首批范围不需要，但当前架构已为核心库复用留出 `pkg/`，演进平滑。
- **微服务：** 拒绝——单实例中等规模不需要，运维复杂度过高。

### Pattern Application

```
客户端（CLI/Web/SDK）
    │ HTTP（tus + REST）
传输层 Transport（net/http + chi）
    │  tus Handler | REST Handler | 下载 Handler | 中间件
    ▼
领域核心 Domain Core
    UploadService（会话/秒传/并发合并/解压/校验）
    DownloadService（流式打包/Range/校验和）
    ▼ 端口/接口
适配层 Adapters（可插拔）
    Storage（本地FS→S3） | Metadata（Redis热+DB冷：SQLite/PG） | Compressor（zstd/gzip/tar） | Hasher（SHA-256）
```

领域核心只依赖 `internal/ports` 的接口；实现在 `internal/adapters/*`；`main` 启动时按配置装配（依赖注入）。单元测试可用 mock 替换全部依赖。

---

## 3. Component Design

### Component Overview

```
┌─────────────────────────────────────────────────────────────┐
│                      CLIENT LAYER                           │
│  ┌───────────────┐                                         │
│  │  Go CLI       │  (cmd/fileupload, 复用 pkg/ 共享库)      │
│  └───────┬───────┘                                         │
└──────────┼──────────────────────────────────────────────────┘
           │ HTTP (tus + REST)
┌──────────▼──────────────────────────────────────────────────┐
│              TRANSPORT LAYER (net/http + chi)               │
│  ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌──────────────────┐  │
│  │tus Hand-│ │REST Hand│ │Download │ │ Middleware        │  │
│  │ler      │ │ler      │ │Handler  │ │ recover/reqID/    │  │
│  │         │ │         │ │         │ │ limit/namespace   │  │
│  └────┬────┘ └────┬────┘ └────┬────┘ └──────────────────┘  │
└───────┼───────────┼───────────┼────────────────────────────┘
        │           │           │
┌───────▼───────────▼───────────▼────────────────────────────┐
│                DOMAIN CORE                                  │
│  ┌──────────────────┐      ┌──────────────────┐            │
│  │ UploadService    │      │ DownloadService  │            │
│  │ (会话/秒传/并发  │      │ (流式打包/Range/ │            │
│  │  合并/解压/校验) │      │  校验和)         │            │
│  └────────┬─────────┘      └────────┬─────────┘            │
│           │                         │                       │
│  ┌────────▼─────────────────────────▼─────────┐            │
│  │ WorkerPool (全局分片处理池)                 │            │
│  └─────────────────────────────────────────────┘            │
└──────────────────────────┬──────────────────────────────────┘
                           │ ports
┌──────────────────────────▼──────────────────────────────────┐
│                  ADAPTER LAYER                              │
│ ┌──────────┐ ┌──────────────────┐ ┌──────────┐ ┌─────────┐ │
│ │ Storage  │ │ Metadata         │ │Compressor│ │ Hasher  │ │
│ │ LocalFS  │ │ Facade           │ │ zstd/    │ │ SHA-256 │ │
│ │ (→S3)    │ │ ├ RedisStore(热) │ │ gzip/tar │ │         │ │
│ │          │ │ └ DBStore(冷)    │ │          │ │         │ │
│ │          │ │   SQLite(→PG)    │ │          │ │         │ │
│ └──────────┘ └──────────────────┘ └──────────┘ └─────────┘ │
└─────────────────────────────────────────────────────────────┘
                           │
┌──────────────────────────▼──────────────────────────────────┐
│                  LIFECYCLE (后台 goroutine)                 │
│  ┌────────────────┐  ┌────────────────────────┐             │
│  │ SessionReaper  │  │ ConsistencyScanner     │             │
│  │ (会话超时清理) │  │ (孤儿/引用计数/损坏巡检)│             │
│  └────────────────┘  └────────────────────────┘             │
└─────────────────────────────────────────────────────────────┘
```

### Component Descriptions

#### Component: tus Handler

**Responsibility:** 实现 tus.io 可续传上传协议（POST 创建 / PATCH 追加分片 / HEAD 查询偏移 / DELETE 取消），翻译为 UploadService 调用。

**Interfaces Provided:**
- `POST /uploads` — 创建上传会话（读 Upload-Length, X-SHA256, X-Compression, X-Chunk-Size 头）
- `HEAD /uploads/{id}` — 返回 Upload-Offset（断点续传）
- `PATCH /uploads/{id}` — 追加一个分片（带 X-Slice-SHA256 校验）
- `DELETE /uploads/{id}` — 取消会话

**Interfaces Required:** UploadService（CreateSession / AppendChunk / GetOffset / Abort）

**NFRs Addressed:** ADR-1（分片流式，不进内存）、ADR-4（共享核心）

---

#### Component: REST Handler

**Responsibility:** 提供自定义 REST 分片上传接口（init/chunk/finalize/status）+ 目录 manifest 提交 + 文件/目录删除 + 列目录 + stat。与 tus 共享同一 UploadService。

**Interfaces Provided:**
- `POST /uploads/init` — 创建会话
- `PUT /uploads/{id}/chunks/{index}` — 上传指定分片
- `GET /uploads/{id}/status` — 已落盘分片索引 + 进度
- `POST /uploads/{id}/finalize` — 触发合并
- `POST /dirs` — 提交目录 manifest，建目录树，返回 dirID
- `DELETE /files/{id}` / `DELETE /dirs/{id}` — 删除
- `GET /ls?parent=...` — 列目录
- `GET /stat/{id}` — 文件信息

**Interfaces Required:** UploadService、DownloadService、Metadata（列目录/stat）

**NFRs Addressed:** ADR-4（共享核心）

---

#### Component: Download Handler

**Responsibility:** 单文件流式下载（带 X-SHA256 + Range）与目录流式打包下载（io.Pipe 管道）。

**Interfaces Provided:**
- `GET /files/{id}` — 单文件下载（支持 Range）
- `GET /dirs/{id}?format=tar.gz` — 目录流式打包下载（响应头含 X-Tree-SHA256）

**Interfaces Required:** DownloadService

**NFRs Addressed:** ADR-1（流式不落临时包）、ADR-2（下载校验和）

---

#### Component: UploadService (Domain Core)

**Responsibility:** 上传编排大脑——会话生命周期、秒传预检、分片并发合并、Finalize 解压 + 整体 SHA-256 校验、content_blobs 去重写入。

**Interfaces Provided:** `CheckExists` / `CreateSession` / `AppendChunk` / `GetOffset` / `Finalize` / `Abort` / `Delete` / `SubmitDir`

**Interfaces Required:** Metadata、Storage、Compressor、Hasher、WorkerPool

**Key Operations:**
1. `CheckExists(sha256, ns)` — 查 content_blobs，命中则 ref_count+1 并建逻辑文件（秒传）。
2. `AppendChunk(...)` — worker 池内校验分片 SHA-256 → 落盘 tmp → Redis Lua 原子更新 offset + chunks。
3. `Finalize(id)` — 按 index 排序合并 tmp 分片 → 解压 → 边算整体 SHA-256 → Storage.Write → 比对 X-SHA256 → 写 content_blobs + files → 迁移会话到 DB → 清理 tmp。

**NFRs Addressed:** ADR-1（流式合并/解压）、ADR-2（整体校验）、ADR-5（去重）

---

#### Component: DownloadService (Domain Core)

**Responsibility:** 下载编排——单文件 Range 读取 + 校验和返回；目录先查 manifest 算 X-Tree-SHA256 写头，再启 io.Pipe 流式打包管道（DirWalker → tar → zstd/gzip → Response）。

**Interfaces Provided:** `GetFile(id, ns, range)` / `StreamDir(dirID, ns, format)`

**Interfaces Required:** Metadata、Storage、Compressor、Hasher

**NFRs Addressed:** ADR-1（流式打包）、ADR-2（Tree-SHA256）

---

#### Component: WorkerPool

**Responsibility:** 全局固定大小分片处理池，所有会话共享，限制并发磁盘 IO；池满返回 503。

**Interfaces Provided:** `Submit(task) error`（带超时排队）

**NFRs Addressed:** ADR-1（并发受控）

---

#### Component: Storage Adapter (LocalFS / →S3)

**Responsibility:** 物理文件读写删查，path = `data/<namespace>/<fileID>`；Open 支持 Range。

**Interfaces Provided:** `Write` / `Open(offset,length)` / `Delete` / `Stat`

**NFRs Addressed:** ADR-3（可插拔）

---

#### Component: Metadata Adapter (Facade: RedisStore + DBStore)

**Responsibility:** 热数据（会话/分片状态/offset，Redis，Lua 原子）+ 冷数据（content_blobs/files，SQLite/PG，引用计数）。

**Interfaces Provided:** 会话 CRUD + offset 原子更新 + blob 引用计数 + file/目录树 CRUD

**NFRs Addressed:** ADR-3（DB 可插拔）、ADR-5（引用计数）

---

#### Component: Compressor Adapter (zstd/gzip/tar)

**Responsibility:** Decompress（Finalize 解压）+ NewArchiveWriter（下载打包：tar→zstd/gzip/zip）。

**NFRs Addressed:** ADR-1（流式压缩）

---

#### Component: Hasher Adapter (SHA-256)

**Responsibility:** Sum（边读边算）+ TeeReader（流式校验）。

**NFRs Addressed:** ADR-2

---

#### Component: SessionReaper

**Responsibility:** 定时（每 1 分钟）扫描 Redis 过期/aborted 会话，删 tmp 分片 + Redis 键；跳过 finalizing 状态。

**NFRs Addressed:** 资源回收（生命周期）

---

#### Component: ConsistencyScanner

**Responsibility:** 低频定时/手动触发，五项检查（孤儿临时分片/孤儿物理文件/元数据孤儿/引用计数漂移/大小哈希不符），默认安全修复 + 隔离 + 报告。

**NFRs Addressed:** 数据一致性（可靠性）

---

## 4. Data Model

### Entity Relationship Diagram

```
┌──────────────────────┐         ┌──────────────────────┐
│   content_blobs      │         │       files          │
│   (内容寻址去重)     │ 1   N   │   (逻辑文件/目录)    │
│──────────────────────│◄────────│──────────────────────│
│ sha256 (PK)          │  refs   │ fileID (PK)          │
│ storage_path         │         │ sha256 (FK→blob)     │
│ size                 │         │ name                 │
│ ref_count            │         │ path                 │
│ created_at           │         │ size                 │
└──────────────────────┘         │ namespace            │
                                 │ is_dir               │
                                 │ parent_id (自引用)   │
                                 │ created_at           │
                                 └──────────────────────┘

┌──────────────────────┐  (Redis 热数据，短生命周期)
│  upload:session:*    │
│──────────────────────│
│ sha256(声明)         │
│ uploadLength         │
│ compression          │
│ chunkSize            │
│ namespace            │
│ createdAt/expireAt   │
│ status               │
└──────────────────────┘
┌──────────────────────┐
│  upload:chunks:*     │  Hash: index→sliceSha256
│  upload:offset:*     │  当前已接收字节偏移
└──────────────────────┘
```

### Entity Specifications

#### Entity: content_blobs

**Purpose:** 内容寻址去重——原始内容 SHA-256 → 物理文件，秒传与去重删除的核心。

**Attributes:**
- `sha256` (TEXT, PK) — 原始内容 SHA-256（hex）
- `storage_path` (TEXT) — `data/<namespace>/<fileID>`
- `size` (BIGINT) — 原始内容字节数
- `ref_count` (INT) — 逻辑文件引用数
- `created_at` (TIMESTAMP)

**Indexes:** PK on sha256

**Constraints:** ref_count ≥ 0；ref_count=0 时可删物理文件

---

#### Entity: files

**Purpose:** 逻辑文件/目录节点，可多个逻辑名指向同一 content_blob。

**Attributes:**
- `fileID` (TEXT, PK)
- `sha256` (TEXT, FK→content_blobs.sha256，目录为 NULL)
- `name` (TEXT)、`path` (TEXT，含目录相对路径)
- `size` (BIGINT)、`namespace` (TEXT)
- `is_dir` (BOOLEAN)
- `parent_id` (TEXT，自引用，目录树)
- `created_at` (TIMESTAMP)

**Indexes:** PK on fileID；index on (namespace, parent_id)（列目录）；index on sha256（引用计数统计）

---

#### Entity: UploadSession (Redis Hash, 非持久表)

**Purpose:** 进行中上传会话状态，短生命周期。

**Attributes:** sessionID、声明 sha256、uploadLength、compression、chunkSize、namespace、createdAt、expireAt、status(active|finalizing|completed|aborted)

---

### Data Storage Strategy

**Primary Database:** SQLite（冷数据，零运维，单文件）；端口抽象，可切换 PostgreSQL。

**Cache (Redis):** 热数据——进行中上传会话、分片索引、offset。offset 更新用 Lua 脚本保证原子性与连续性。建议 AOF 持久化（每秒 fsync）防会话丢失。

**File Storage:** 本地文件系统，`data/<namespace>/<fileID>` 存原始未压缩数据；`tmp/<sessionID>/` 存分片临时 .part；`quarantine/` 存巡检隔离文件。端口抽象，可切换 S3。

**Data Retention:** 无自动过期；手动删除 + 引用计数；上传会话 1 小时无活动过期清理。

**Backup Strategy:** 冷数据 SQLite 文件 + data/ 目录定期备份（部署阶段定义）。

---

## 5. API Specifications

### API Design Approach

**Protocol:** HTTP（tus 协议 + REST 双协议）
**Authentication:** 无（网关代理，上游注入 `X-User-ID` 作为 namespace）
**Versioning:** URL 前缀 `/v1`（REST 部分）；tus 部分遵循 tus.io 协议头

### Endpoint Groups

#### Group 1: 秒传预检

##### `HEAD /v1/files?sha256={hex}&namespace={ns}`

**Purpose:** 上传前检查内容是否已存在（秒传）
**Response 200:** `{ "fileID": "...", "sha256": "...", "size": N }`（命中，复用）
**Response 404:** 未命中，走正常上传
**NFRs:** ADR-5

---

#### Group 2: tus 上传

##### `POST /uploads`
创建会话。头：`Upload-Length`、`X-SHA256`（原始内容哈希）、`X-Compression: zstd|none`、`X-Chunk-Size`。
**Response 201:** `Location: /uploads/{id}`

##### `HEAD /uploads/{id}`
**Response 200:** `Upload-Offset: <bytes>`（断点续传用）

##### `PATCH /uploads/{id}`
追加分片，头 `Upload-Offset`、`X-Slice-SHA256`（压缩分片哈希），body 为分片字节。
**Response 204:** `Upload-Offset` 更新
**Error 460/409:** 分片校验失败 / offset 冲突（重传该片）

##### `DELETE /uploads/{id}`
取消会话。**Response 204**

---

#### Group 3: REST 上传

##### `POST /v1/uploads/init` — 创建会话（同 tus 创建语义）
##### `PUT /v1/uploads/{id}/chunks/{index}` — 上传指定 index 分片（带 X-Slice-SHA256）
##### `GET /v1/uploads/{id}/status` — 返回已落盘分片索引 + 进度
##### `POST /v1/uploads/{id}/finalize` — 触发合并+解压+校验
  **Response 200:** `{ "fileID": "...", "sha256": "...", "size": N }`
  **Error 422:** 整体校验失败（整文件重传）
##### `DELETE /v1/uploads/{id}` — 取消

##### `POST /v1/dirs` — 提交目录 manifest
  **Request:** `{ "entries": [ { "path": "rel/path", "fileID": "..." } ] }`
  **Response 200:** `{ "dirID": "..." }`

---

#### Group 4: 下载

##### `GET /v1/files/{id}?namespace={ns}`
单文件流式下载，支持 `Range`。
**Response 200/206:** `X-SHA256: <hex>`, `Content-Length`, 流式 body

##### `GET /v1/dirs/{id}?format=tar.gz&namespace={ns}`
目录流式打包下载。
**Response 200:** `Content-Type: application/gzip`, `X-Tree-SHA256: <hex>`, `Content-Disposition`, 流式 body

---

#### Group 5: 管理操作

##### `DELETE /v1/files/{id}` / `DELETE /v1/dirs/{id}` — 删除（目录递归，引用计数）
##### `GET /v1/ls?parent={id|root}&namespace={ns}` — 列目录子节点
##### `GET /v1/stat/{id}` — 文件信息（sha256/size/ref_count）
##### `POST /v1/admin/scan` — 触发一致性巡检

---

### API Security

**Authentication:** 服务端不做鉴权，依赖上游网关注入 `X-User-ID`（namespace）。所有查询/写入按 namespace 过滤隔离。
**Rate Limiting:** 传输层中间件按 namespace/IP 限流（令牌桶），worker 池满返回 503 + Retry-After。
**Input Validation:** 路径合法性校验（防路径穿越）、分片 index/offset 连续性校验、SHA-256 格式校验。

---

## 6. Non-Functional Requirements Mapping

### NFR Coverage Matrix

| NFR ID | Category | Requirement | Architectural Decision | Status |
|--------|----------|-------------|----------------------|--------|
| ADR-1 | Performance | 1–10TB，数百级并发 | Go + 分片级并发 + worker 池 + 全程流式 IO（io.Pipe） | ✓ Addressed |
| ADR-2 | Reliability | 数据完整性防篡改/损坏 | 四点 SHA-256 校验（分片/整体/秒传/下载） | ✓ Addressed |
| ADR-3 | Maintainability | 基础设施可插拔 | 端口/适配器分层，核心不依赖实现 | ✓ Addressed |
| ADR-4 | Maintainability | 双协议不重复逻辑 | tus + REST 共享 UploadService 核心 | ✓ Addressed |
| ADR-5 | Performance/Storage | 秒传去重省存储 | content_blobs 内容寻址 + 引用计数 | ✓ Addressed |
| NFR-OPS | Operability | 资源回收与一致性 | SessionReaper + ConsistencyScanner | ✓ Addressed |
| NFR-CONC | Concurrency | 并发安全 | Redis Lua 原子 offset + 分片乱序落盘 + -race 测试 | ✓ Addressed |
| NFR-SEC | Security | 多租户隔离 | namespace 隔离（网关注入） | ✓ Addressed |

### Detailed NFR Implementations

#### Performance (ADR-1)
- 全程流式：上传分片落盘流式、Finalize 合并/解压/校验 io.Pipe 串联、下载打包 io.Pipe 管道，大文件不进内存。
- worker 池固定大小（≈GOMAXPROCS），全进程共享，避免磁盘 IO 打爆。
- Redis 热数据低延迟查询；DB 冷数据按需查。

#### Reliability (ADR-2)
- 分片校验：每个 PATCH 落盘后算 X-Slice-SHA256 比对，失败重传该片。
- 整体校验：Finalize 边合并边算整体 SHA-256，比对 X-SHA256，失败删数据 422。
- 下载校验：响应头返回 X-SHA256 / X-Tree-SHA256，客户端下载后比对。

#### Maintainability (ADR-3, ADR-4)
- 四端口接口固定，实现可替换；核心依赖接口，单测 mock 全替换。
- 双协议收敛到 UploadService，业务逻辑单点。

#### Concurrency Safety (NFR-CONC)
- offset 用 Redis Lua 原子更新 + 连续性校验。
- 分片乱序到达按 index 落盘，合并时排序。
- 全测试 `go test -race`。

---

## 7. Technology Stack

### Backend

**Language:** Go（最新稳定版）
**Rationale:** 高并发、流式/切片处理是 Go 强项；单二进制部署；GC 压力可控；net/http 标准库足够。

**HTTP Framework:** net/http + chi
**Rationale:** 轻量、依赖少、与标准库兼容；tus 协议需自定义处理，chi 路由足够。
**Alternatives:** Gin（生态成熟但中间件/tus 适配稍重）、Fiber（fasthttp 与 net/http 不完全兼容，tus 适配有坑）。

**Key Libraries:**
- `github.com/go-chi/chi` — 路由
- `github.com/redis/go-redis/v9` — Redis 客户端
- `github.com/redis/go-redis/v9` + `github.com/alicebob/miniredis` — 测试用嵌入式 Redis
- `github.com/klauspost/compress/zstd` — zstd 压缩
- `modernc.org/sqlite` 或 `github.com/mattn/go-sqlite3` — SQLite（纯 Go 优先 modernc 避免 CGO）
- `github.com/google/uuid` — ID 生成

---

### Database

**Primary (冷数据):** SQLite（起步，纯 Go 驱动 modernc.org/sqlite，免 CGO）
**Cache (热数据):** Redis
**Rationale:** SQLite 零运维、单文件、事务可靠，匹配中等规模单机；Redis 热数据低延迟。端口抽象支持后续切 PG。

---

### Storage

**File Storage:** 本地文件系统（起步），端口抽象支持 S3。

---

### Infrastructure

**Deployment:** 单实例二进制 + 本地磁盘 + Redis + SQLite 文件。上游网关（Nginx/Gateway）负责鉴权与入口。
**Monitoring:** 结构化日志（requestID 串联）+ 健康检查端点。

---

### Development & Deployment

**Version Control:** Git
**Build:** `go build ./cmd/server` / `go build ./cmd/fileupload`
**Testing:** `go test -race ./...`
**Containerization:** Dockerfile（后续阶段）

---

## 8. Trade-off Analysis

### Trade-off 1: tus + REST 双协议 vs 单协议

**Decision:** 双协议共享 UploadService 核心。
**Options:**
1. 双协议（选）— Pros: 覆盖 tus 生态客户端 + 自定义场景；Cons: 传输层两套 handler。
2. 仅 tus — Cons: 自定义场景受限。
3. 仅 REST — Cons: 失去 tus 生态与标准续传客户端。
**Selection Rationale:** 用户明确要求双协议；共享核心把 Cons（逻辑重复）消除。**Mitigation:** 传输层薄，核心单一。

### Trade-off 2: Redis 热 + DB 冷 vs 单一存储

**Decision:** Redis 热数据（会话/分片/offset）+ SQLite/PG 冷数据（blob/file）。
**Options:**
1. 分层（选）— Pros: 热数据低延迟、冷数据可靠持久；Cons: 两套存储、门面复杂度。
2. 全 Redis — Cons: 持久化/内存容量/冷数据查询弱。
3. 全 DB — Cons: 热数据 offset 高频更新压力。
**Selection Rationale:** 用户选择 Redis 热数据 + DB 冷数据。**Mitigation:** Metadata 门面封装路由；Redis AOF 持久化防丢失。

### Trade-off 3: 存储只存原始数据 vs 存压缩数据

**Decision:** 存原始未压缩数据。
**Options:**
1. 存原始（选）— Pros: 去重按原始 SHA-256 语义清晰、下载可按需选格式；Cons: 存储空间略大。
2. 存压缩 — Cons: 去重哈希语义混乱、下载格式受限。
**Selection Rationale:** 秒传去重要求内容寻址，原始数据哈希一致最清晰。**Mitigation:** 存储透明压缩可在 Storage 适配器内做（后续）。

### Trade-off 4: 目录下载流式打包 vs 先打包

**Decision:** 流式 io.Pipe 打包，不落临时文件。
**Options:**
1. 流式（选）— Pros: 省磁盘、低延迟；Cons: 已发头后出错无法改状态码（靠校验和感知）。
2. 先打包 — Cons: 占磁盘、首次延迟高。
**Selection Rationale:** 中等规模省磁盘与延迟优先。**Mitigation:** X-Tree-SHA256 头先算好，客户端可校验完整性。

### Trade-off 5: SQLite vs PostgreSQL 起步

**Decision:** SQLite 起步 + PG 端口预留。
**Selection Rationale:** 零运维、单文件、匹配单机中等规模；DBStore 接口抽象，后续切 PG 平滑。**Revisit:** 多节点扩展时切 PG。

---

## 9. Deployment Architecture

### Environments

**Development:** 本地 `go run`，miniredis 内存 + SQLite 临时文件 + 临时 data 目录。
**Production:** 单二进制 + 本地磁盘 + Redis 实例 + SQLite 文件；上游网关注入 X-User-ID。

### Production Deployment

```
                    ┌──────────────┐
                    │   Gateway    │  (鉴权 + 注入 X-User-ID)
                    └──────┬───────┘
                           │
                    ┌──────▼───────┐
                    │  fileupload  │  (单二进制, net/http)
                    │   server     │
                    └──┬────────┬──┘
                       │        │
              ┌────────▼──┐  ┌──▼──────────┐
              │  Redis    │  │  SQLite     │
              │ (热数据)  │  │  (冷数据)   │
              └───────────┘  └─────────────┘
                       │
              ┌────────▼──────────┐
              │  本地磁盘          │
              │  data/<ns>/<fileID>│
              │  tmp/<sessionID>/  │
              │  quarantine/       │
              └────────────────────┘
```

### Deployment Strategy

**Method:** 滚动/蓝绿（单实例起步可简单重启）。
**Rollback:** 二进制回退版本；data/ 与 SQLite 向后兼容（迁移脚本）。

### Scaling Strategy

**Horizontal:** 端口抽象支持后续多实例（Redis 共享会话、PG 共享冷数据、S3 共享存储）。
**Current:** 单实例足够中等规模；worker 池内并发已控。

---

## 10. Future Considerations

### Anticipated Changes

**Near Term:**
- S3 Storage 适配器：实现 Storage 端口，配置切换。
- PostgreSQL DBStore：实现 DBStore 子接口，多节点铺路。

**Medium Term:**
- Web UI：拖拽上传、目录树、进度。
- 多语言 SDK：基于 `pkg/` 共享核心经 gRPC 网关/FFI 暴露。

**Long Term:**
- 多节点水平扩展：分布式 worker 池、跨节点分片、PG + S3 共享。

### Scalability Path

**Current:** 单实例 1–10TB。
**Scale to 多节点:** Redis 共享会话 + PG 共享冷数据 + S3 共享存储 + 无状态 app 实例横向扩展。

### Technology Evolution

- SQLite → PostgreSQL：DBStore 接口已抽象，切换成本低。
- 本地 FS → S3：Storage 接口已抽象。
- zstd → 其他压缩：Compressor 端口支持新增 Format。

---

## Appendix

### Glossary

| Term | Definition |
|------|------------|
| namespace | 多租户隔离标识，由网关注入 X-User-ID |
| content_blobs | 内容寻址去重表，原始 SHA-256 → 物理文件 |
| 秒传 | 上传前按原始内容 SHA-256 查重，命中则复用，免传输 |
| 断点续传 | 中断后从已接收 offset 继续上传 |
| Finalize | 全部分片到位后合并+解压+校验+落盘的编排步骤 |
| SessionReaper | 定时清理过期上传会话及临时分片 |
| ConsistencyScanner | 定时/手动巡检存储与元数据一致性 |

### References

- 设计文档: `docs/superpowers/specs/2026-06-17-fileupload-design.md`
- tus 协议: https://tus.io/protocols/resumable-upload

### Document History

| Version | Date | Author | Changes |
|---------|------|--------|---------|
| 1.0 | 2026-06-17 | System Architect | 初始架构文档，基于设计 spec 产出 |

---

**END OF DOCUMENT**
