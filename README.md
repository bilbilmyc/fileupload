# fileupload — 文件上传下载服务

[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

高性能、可自托管的文件上传下载服务。支持动态分片、客户端压缩、流式打包下载、SHA-256 全链路数据校验、断点续传、秒传（内容寻址去重）。用 Go 编写，单二进制部署，内置 React 管理面板。

---

## 功能特性

| 特性 | 说明 |
|------|------|
| **双协议上传** | tus.io 协议 + 自定义 REST API，共享同一领域核心 |
| **动态分片** | 大文件自动切分，并发上传 |
| **客户端压缩** | zstd 压缩后传输，服务端透明解压 |
| **流式下载** | 单文件 Range 分段下载，目录/批量流式打包（tar.gz / zip） |
| **数据校验** | 分片级 + 整体级 SHA-256，传输与存储全链路防篡改 |
| **秒传** | 内容寻址去重（content-addressed storage），相同内容秒级完成 |
| **断点续传** | tus 协议原生 + REST 客户端 resume 状态文件 |
| **并发控制** | 全局 worker 池，限制并发磁盘 IO |
| **命名空间隔离** | 多租户隔离（由上游 Gateway 注入 `X-Namespace`） |
| **层级存储** | 目录上传后物理文件按原始层级路径存放，可直接从文件系统拷贝 |
| **文件标签** | 关系型标签系统，支持批量标记与分类 |
| **批量操作** | 批量删除 / 打包下载 / 移动 / 复制 / 标记，可选目录目标 |
| **一致性巡检** | 定时/手动扫描孤儿文件、引用计数漂移 |
| **Go CLI** | Cobra 命令行客户端，支持 kubectl 式 Tab 补全 |
| **Web UI** | React + Ant Design 管理面板，完整面包屑导航、拖拽上传、进度跟踪、批量管理 |
| **登录预留** | 前端已实现登录页与 token 管理，后端鉴权可后续接入 |

---

## 快速开始

### 前提条件

- Go 1.25+
- Node.js 20+（构建前端）
- Redis（上传会话热数据，启动失败则仅热数据功能受限）

### 从源码运行

```bash
# 克隆项目
git clone https://github.com/bilbilmyc/fileupload.git
cd fileupload

# 构建前端（开发时也可用 npm run dev 单独启动）
make web

# 启动服务端（默认监听 :8080，使用 SQLite + 本地文件系统）
go run ./cmd/server

# 另开终端 — 上传文件
go run ./cmd/fileupload upload README.md

# 下载文件
go run ./cmd/fileupload download <fileID> -o output.md

# 打开浏览器管理面板
open http://localhost:8080
```

### 前端开发模式

```bash
# 前端独立开发（热更新，代理到 localhost:8080）
cd web
npm install
npm run dev
```

### 使用 Docker Compose

```bash
cd deploy/docker
docker compose up -d
# 服务端运行在 http://localhost:8080
```

---

## CLI 使用

所有子命令均支持 `--server` 指定服务端地址（默认 `http://localhost:8080`）和 `--namespace` 指定命名空间（默认 `default`）。

```bash
# === 上传 ===
# 单文件上传（自动分片、zstd 压缩、进度条）
fileupload upload large-file.dat --concurrency 8 --compress zstd

# 上传整个目录（递归遍历 + manifest 提交）
fileupload upload ./my-dir/

# === 下载 ===
# 单文件下载（自动 SHA-256 校验）
fileupload download abc123 -o output.bin

# 目录流式打包下载
fileupload download dir_abc -o project.tar.gz --format tar.gz

# === 信息与管理 ===
# 文件/目录元信息
fileupload stat abc123

# 列目录
fileupload ls /
fileupload ls parent_dir_id

# 删除文件或目录
fileupload rm abc123

# 上传会话状态查询（断点续传用）
fileupload status sess_id

# === 高级 ===
# 服务端一致性巡检
fileupload scan

# 压测（50 个文件，每个 100MB，16 并发）
fileupload bench --files 50 --size 100m --concurrency 16

# 自定义服务端地址
fileupload --server http://192.168.1.100:8080 upload data.bin
```

### 子命令一览

| 子命令 | 功能 | 关键参数 |
|--------|------|---------|
| `upload` | 上传文件或目录 | `--concurrency`, `--compress`, `--chunk-size` |
| `download` | 下载文件或目录 | `-o`, `--range`, `--format` |
| `rm` | 删除文件或目录 | — |
| `ls` | 列目录 | — |
| `stat` | 查看文件/目录信息 | — |
| `status` | 查询上传会话进度 | — |
| `scan` | 触发服务端一致性巡检 | — |
| `bench` | 压测 | `--files`, `--size`, `--concurrency` |
| `config` | 查看当前配置 | — |
| `login` | 登录占位（预留） | — |
| `completion` | 生成 shell 补全脚本 | `bash\|zsh\|fish\|powershell` |

### kubectl 式 Tab 补全

Cobra 支持为命令、子命令和 flag 生成 shell 补全脚本。

```bash
# bash
source <(fileupload completion bash)

# zsh
source <(fileupload completion zsh)

# fish
fileupload completion fish | source
```

配置完成后，连续按 Tab 即可补全子命令与参数：

```bash
fileupload up<TAB>      # → upload
fileupload upload --co<TAB>  # → --concurrency, --compress, --chunk-size
```

---

## Web UI

前端位于 `web/` 目录，技术栈：

- Vite 6
- React 18 + TypeScript
- Tailwind CSS
- Ant Design 5
- React Router 6
- Axios

### 功能

- **目录浏览** — 完整面包屑路径导航，每级可点击返回，文件列表显示 `..` 返回上级
- **拖拽上传** — 始终展开的紧凑上传条，支持单文件/目录模式，实时进度与速率
- **批量管理** — 选中多文件后批量删除/下载（zip/tar.gz）/移动/复制/标记
- **目录选择器** — 树形浏览+搜索过滤，用于批量移动/复制目标选择
- **标签系统** — 彩色标签编辑，快速添加常用标签，逗号分隔批量输入
- **操作历史** — 表格分页展示批量操作记录
- **文件管理** — 搜索过滤、分页、下载、删除，目录大小递归显示

### 构建

```bash
cd web
npm install
npm run build
```

构建产物输出到 `web/dist/`，由 Go 二进制通过 `embed` 直接提供。

---

## API 参考

### tus 协议（可续传上传）

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/uploads` | 创建上传会话 |
| `HEAD` | `/uploads/{id}` | 查询上传进度（Upload-Offset） |
| `PATCH` | `/uploads/{id}` | 追加分片 |
| `DELETE` | `/uploads/{id}` | 取消上传 |

### REST API

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/v1/uploads/init` | 创建会话 |
| `PUT` | `/v1/uploads/{id}/chunks/{index}` | 上传分片 |
| `GET` | `/v1/uploads/{id}/status` | 查询分片进度 |
| `POST` | `/v1/uploads/{id}/finalize` | 完成上传（合并+校验+去重写入） |
| `HEAD` | `/v1/files?sha256={hex}` | 秒传预检（按内容哈希去重） |
| `GET` | `/v1/files/{id}` | 下载文件（支持 Range） |
| `GET` | `/v1/dirs/{id}` | 下载目录（流式打包） |
| `POST` | `/v1/dirs` | 提交目录 manifest |
| `DELETE` | `/v1/files/{id}` | 删除文件或目录 |
| `GET` | `/v1/ls` | 列目录（含祖先链 breadcrumb） |
| `GET` | `/v1/stat/{id}` | 文件/目录信息 |

### 批量操作 API

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/v1/batch/delete` | 批量删除文件/目录 |
| `POST` | `/v1/batch/download` | 批量下载（流式 zip/tar.gz 打包） |
| `POST` | `/v1/batch/move` | 批量移动到目标目录 |
| `POST` | `/v1/batch/copy` | 批量复制到目标目录（含目录递归） |
| `POST` | `/v1/batch/tags` | 批量设置标签 |

### 管理 API

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/v1/admin/scan` | 触发一致性巡检 |
| `GET` | `/health` | 健康检查 |

### HTTP 请求头

| 头 | 适用 API | 说明 |
|------|----------|------|
| `X-Namespace` | 全部 | 命名空间（多租户隔离，缺省为 `default`） |
| `Authorization` | 全部 | 预留：Bearer token（当前服务端未校验） |
| `X-SHA256` | 上传 | 原始内容的 SHA-256（用于最终校验和秒传） |
| `X-Compression` | 上传 | 客户端压缩格式：`none` / `zstd` |
| `X-File-Name` | 上传 | 原始文件名 |
| `X-Slice-SHA256` | 上传分片 | 当前分片的 SHA-256（服务端校验） |
| `Range` | 下载 | HTTP Range 头：`bytes=0-1023` |

### 错误码

| HTTP 状态码 | 含义 |
|-------------|------|
| 400 | 参数不合法 |
| 404 | 资源不存在 |
| 403 | 命名空间无权限 |
| 460 | 分片 SHA-256 校验失败 |
| 422 | 整体 SHA-256 校验失败 |
| 503 | 服务忙（worker 池满） |
| 410 | 文件已损坏 |

---

## 配置

配置文件为 YAML 格式（默认读取当前目录下的 `fileupload.yaml`），或通过环境变量覆盖。

### 完整配置项

```yaml
server:
  addr: ":8080"              # 监听地址
  read_timeout: 30           # 读取超时（秒）
  write_timeout: 300         # 写入超时（秒），大文件需长超时
  idle_timeout: 60           # 空闲超时（秒）

storage:
  type: "local"              # 存储类型：local / s3（预留）
  data_dir: "storage/data"   # 数据目录
  temp_dir: "storage/tmp"    # 临时分片目录

redis:
  addr: "localhost:6379"     # Redis 地址
  password: ""               # 密码
  db: 0                      # 数据库编号
  prefix: "upload:"          # key 前缀

database:
  type: "sqlite"             # 数据库类型：sqlite / postgres（预留）
  path: "storage/fileupload.db"  # SQLite 文件路径

upload:
  session_ttl_minutes: 60    # 会话超时（分钟）
  default_chunk_size: 10485760  # 分片大小（字节），10MB
  worker_pool_size: 4        # worker 池大小
  worker_queue_size: 100     # 排队上限

download:
  max_archive_size: 0        # 打包上限（0=不限）
```

### 环境变量覆盖

```bash
FILEUPLOAD_CONFIG=/etc/fileupload/config.yaml
FILEUPLOAD_REDIS_ADDR=redis.example.com:6379
FILEUPLOAD_REDIS_PASSWORD=secret
FILEUPLOAD_DB_PATH=/data/fileupload.db
FILEUPLOAD_STORAGE_DATA_DIR=/data/files
FILEUPLOAD_CHUNK_SIZE=20971520
```

---

## 架构概览

```
客户端 (CLI / Web / SDK)
      │ HTTP (tus + REST)
┌─────▼─────────────────────┐
│ 传输层 (net/http)          │
│ tus Handler | REST Handler│
│ 下载 Handler | Batch      │
│ Handler | 中间件           │
└─────┬─────────────────────┘
      ▼
┌──────────────────────────┐
│ 领域核心                  │
│ UploadService / Download │
│ BatchService / WorkerPool│
│ 领域模型                  │
└─────┬─────────────────────┘
      │ 端口接口（port）
┌─────▼─────────────────────┐
│ 适配层（可插拔）           │
│ Storage / Metadata        │
│ Compressor / Hasher       │
└─────┬─────────────────────┘
      │
  ┌───┴───┬──────────┐
  │ 磁盘  │ Redis   │ SQLite
```

### 文件存储策略

物理文件采用**双层存储策略**：

1. **上传阶段**：文件内容以 `namespace/filename` 扁平路径写入（内容寻址）
2. **目录提交后**：SubmitDir 将文件搬移到 `namespace/dir/subdir/filename` 层级路径
3. **元数据层**：SQLite 维护完整的目录树结构（parent_id 自引用），包含文件标签（file_tags 关联表）

这样既能通过文件系统直接拷贝目录，又不影响上传性能。

完整设计文档见 [docs/adr/](docs/adr/)

---

## 项目结构

```
.
├── cmd/
│   ├── server/               # 服务端入口（HTTP 服务）
│   └── fileupload/           # CLI 客户端（Cobra 子命令）
├── internal/
│   ├── domain/               # 领域核心
│   │   ├── model.go          # 领域模型（UploadSession / FileMetadata / ContentBlob）
│   │   ├── ports.go          # 端口接口（Storage / Metadata / Compressor）
│   │   ├── upload.go         # 上传编排（会话/分片/Finalize/秒传/删除/SubmitDir）
│   │   ├── download.go       # 下载编排（Range/目录遍历/流式打包/祖先链）
│   │   ├── batch.go          # 批量操作（删除/下载/移动/复制/标签）
│   │   └── worker_pool.go    # 并发 worker 池
│   ├── adapters/             # 适配层实现
│   │   ├── storage/          # 本地文件系统存储 + S3
│   │   ├── metadata/         # Redis 热数据 + SQLite 冷数据门面
│   │   ├── compressor/       # zstd/gzip/tar/zip 压缩
│   │   └── hasher/           # SHA-256 哈希
│   ├── transport/            # HTTP 传输层
│   │   ├── router.go         # 路由注册 + 嵌入 React 构建产物
│   │   ├── tus.go            # tus/REST/下载 handler
│   │   ├── batch.go          # 批量操作 handler
│   │   └── middleware.go     # 中间件（Recover/RequestID/Namespace/RateLimit）
│   ├── lifecycle/            # SessionReaper + ConsistencyScanner
│   └── config/               # 配置加载（YAML + env override）
├── web/                      # React 前端（Vite + React + Ant Design）
│   ├── src/
│   │   ├── pages/            # Files.tsx, Login.tsx
│   │   ├── components/       # 13 个 UI 组件
│   │   ├── hooks/            # useFileOperations, useUpload
│   │   └── api/              # API 客户端
├── deploy/
│   ├── docker/               # Dockerfile + Compose
│   └── systemd/              # systemd 服务单元
├── docs/
│   ├── adr/                  # 架构决策记录（5 份）
│   └── agents/               # Agent 配置
├── AGENTS.md                 # Agent 技能配置
├── CONTEXT.md                # 领域词汇表
├── Makefile                  # 编译/测试/发布/前端构建
├── fileupload.yaml           # 默认配置文件
└── README.md
```

---

## 编译

### 当前平台

```bash
make web      # 构建前端
make server   # 编译服务端
make cli      # 编译 CLI
```

### 全平台交叉编译

```bash
make release
```

产物在 `build/` 目录：

```
build/
├── fileupload-server-linux-amd64
├── fileupload-server-linux-arm64
├── fileupload-server-darwin-amd64
├── fileupload-server-darwin-arm64
├── fileupload-cli-linux-amd64
├── fileupload-cli-linux-arm64
├── fileupload-cli-darwin-amd64
└── fileupload-cli-darwin-arm64
```

---

## 测试

```bash
# 全部测试
go test ./...

# 包含覆盖率
go test -cover ./...

# 包含 race 检测
go test -race ./...

# E2E 测试（全链路：真实 HTTP → 真实存储 → 真实数据库）
go test -v -run TestE2E ./internal/transport/
```

---

## 部署

### systemd（Linux）

```bash
sudo cp deploy/systemd/fileupload-server.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now fileupload-server
sudo journalctl -u fileupload-server -f
```

### Docker

```bash
# 手动构建
make docker

# 或使用 Compose（含 Redis）
cd deploy/docker && docker compose up -d
```

### 生产环境建议

- 前端放 Gateway / Nginx 做 TLS 终结和鉴权
- 数据目录可直接通过文件系统拷贝（层级存储）
- Redis 启用 AOF 持久化
- 定期执行 `fileupload scan` 或开启自动巡检
- 数据目录和 SQLite 文件定期备份
- 调整 `worker_pool_size` 和 `write_timeout` 适应大文件并发

---

## 开发

```bash
# 代码检查
make vet
make lint

# 运行测试
make test

# 单包测试
go test -v -run TestUpload ./internal/domain/

# 前端开发
cd web && npm run dev
```

---

## 架构决策

关键架构决策记录在 [docs/adr/](docs/adr/)：

| ADR | 决策 |
|-----|------|
| 0001 | **物理文件存储策略** — Finalize 扁平写入 + SubmitDir 搬移到层级路径 |
| 0002 | **文件标签存储方案** — file_tags 关联表（关系型） |
| 0003 | **SubmitDir 复用 Finalize 记录** — 不重复创建元数据 |
| 0004 | **目录上传目录树自动构建** — 解析路径分隔符创建中间节点 |
| 0005 | **批量下载支持 zip** — Go 标准库 `archive/zip` 流式打包 |

---

## 登录与鉴权（预留）

- 前端已包含 `AuthContext`、登录页、路由守卫和 `Authorization: Bearer` 请求头。
- 后端当前不校验 token，namespace 由 `X-Namespace` 头或 `X-User-ID`（网关）提供。
- 后续接入真实鉴权时：
  1. 在 `internal/transport` 新增 JWT 验证中间件；
  2. 从 token 解析 `user_id` 和 `namespace` 注入 context；
  3. 移除/限制 `X-Namespace` 客户端直接设置；
  4. 将 `/v1/auth/me` 替换为真实登录/刷新接口。

---

## SDK

fileupload 提供多语言 SDK，方便其他服务集成：

### Go SDK

独立包 [`github.com/bilbilmyc/fileupload/sdk/go/fileupload`](sdk/go/fileupload)：

```go
client := fileupload.NewClient("http://localhost:8080")
info, err := client.Upload(ctx, "data.bin", fileupload.WithCompression("zstd"))
```

### TypeScript SDK

npm 包 [`@fileupload/sdk`](sdk/js)：

```typescript
const client = new FileuploadClient({ endpoint: 'http://localhost:8080' })
const file = await client.upload(fileBlob, 'photo.jpg')
```

详见 [sdk/README.md](sdk/README.md)。

---

## 许可证

MIT
