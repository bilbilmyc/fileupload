# fileupload — 文件上传下载服务

[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

高性能、可自托管的文件上传下载服务。支持动态分片、客户端压缩、流式打包下载、SHA-256 全链路校验、断点续传、秒传（内容寻址去重）、文件分享。单二进制部署，内置 React 管理面板。

**版本**：v0.4.0+（含 Prometheus 指标、Grafana 仪表盘、Alertmanager 告警、Go/JS 双 SDK）

---

## 快速开始

### 前提条件

- Go 1.25+
- Node.js 20+（构建前端）

### CI/CD 概览

- 触发：push 到 main / PR / tag `v*`
- 产物：4 平台 × server + CLI = 8 个二进制 + 2 架构 Docker 镜像
- Tag push 自动创建 GitHub Release（含 release notes）
- 详细文档：[docs/ci.md](docs/ci.md)
- Redis（上传会话热数据）
- SQLite（内置，零配置）或 PostgreSQL（生产推荐）

### 从源码运行

```bash
# 构建前端
make web

# 启动服务端（默认 :8080，SQLite + 本地文件系统）
go run ./cmd/server

# 上传文件
go run ./cmd/fileupload upload README.md

# 下载文件
go run ./cmd/fileupload download <fileID> -o output.md

# 打开管理面板
open http://localhost:8080
```

### 使用 Docker Compose

```bash
cd deploy/docker
docker compose up -d
# 服务端运行在 http://localhost:8080，含 Redis
```

---

## 配置

配置文件为 YAML 格式（默认读取 `fileupload.yaml`），支持环境变量覆盖。

### 完整配置

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
  type: "sqlite"             # 数据库类型：sqlite / postgres
  path: "fileupload.db"      # SQLite 文件路径（仅 sqlite 模式）
  # PostgreSQL 连接配置（仅 postgres 模式，结构体形式避免 DSN 转义）
  pg:
    host: "localhost"
    port: 5432
    user: "postgres"
    password: "your_password_here"     # 直接填写原始密码，无需 URI 转义
    dbname: "fileupload"
    sslmode: "disable"

upload:
  session_ttl_minutes: 60    # 会话超时（分钟）
  default_chunk_size: 10485760  # 分片大小（字节），10MB
  worker_pool_size: 10       # worker 池大小
  worker_queue_size: 100     # 排队上限

auth:
  enabled: false             # 是否启用 X-Auth-Token 认证
  token: ""                  # 静态令牌
  header: "X-Auth-Token"     # 令牌请求头
  jwt_secret: ""             # JWT 签名密钥（非空时启用 JWT 鉴权）
  jwt_expiry: 72             # JWT token 过期时间（小时）

download:
  max_archive_size: 0        # 目录打包大小上限（0 = 不限）
```

### PostgreSQL 密码说明

`database.pg.password` 使用**结构体字段**而非 DSN 字符串，密码中的特殊字符（`@` `#` `?` `&` 等）无需任何转义：

```yaml
# 正确 — 直接填写原始密码
pg:
  password: "Postgres@2026#secure"
```

服务端内部使用 `net/url.UserPassword` 自动构建 DSN，将密码进行 URL 编码。

### 环境变量

| 变量 | 对应配置 | 示例 |
|------|---------|------|
| `FILEUPLOAD_CONFIG` | 配置文件路径 | `/etc/fileupload/config.yaml` |
| `FILEUPLOAD_SERVER_ADDR` | server.addr | `:8080` |
| `FILEUPLOAD_STORAGE_DATA_DIR` | storage.data_dir | `/data/files` |
| `FILEUPLOAD_STORAGE_TEMP_DIR` | storage.temp_dir | `/data/tmp` |
| `FILEUPLOAD_REDIS_ADDR` | redis.addr | `redis.example.com:6379` |
| `FILEUPLOAD_REDIS_PASSWORD` | redis.password | `secret` |
| `FILEUPLOAD_DATABASE_TYPE` | database.type | `sqlite` / `postgres` |
| `FILEUPLOAD_DB_PATH` | database.path | `/data/fileupload.db` |
| `FILEUPLOAD_PG_HOST` | database.pg.host | `10.0.0.1` |
| `FILEUPLOAD_PG_PORT` | database.pg.port | `5432` |
| `FILEUPLOAD_PG_USER` | database.pg.user | `postgres` |
| `FILEUPLOAD_PG_PASSWORD` | database.pg.password | `Postgres@2026` |
| `FILEUPLOAD_PG_DBNAME` | database.pg.dbname | `fileupload` |
| `FILEUPLOAD_PG_SSLMODE` | database.pg.sslmode | `disable` / `require` |
| `FILEUPLOAD_SESSION_TTL` | upload.session_ttl_minutes | `120` |
| `FILEUPLOAD_CHUNK_SIZE` | upload.default_chunk_size | `20971520` |
| `FILEUPLOAD_WORKER_POOL` | upload.worker_pool_size | `20` |
| `FILEUPLOAD_AUTH_TOKEN` | auth.token（同时启用 auth） | `my-token` |
| `FILEUPLOAD_AUTH_HEADER` | auth.header | `X-Api-Key` |

---

## CLI 使用

```bash
# 单文件上传（自动分片、zstd 压缩、进度条）
fileupload upload large-file.dat --concurrency 8 --compress zstd

# 上传目录（递归遍历 + manifest 提交）
fileupload upload ./my-dir/

# 下载文件（自动 SHA-256 校验）
fileupload download abc123 -o output.bin

# 目录流式打包下载
fileupload download dir_abc -o project.tar.gz --format tar.gz

# 列目录
fileupload ls /
fileupload ls parent_dir_id

# 文件信息
fileupload stat abc123

# 删除文件或目录
fileupload rm abc123

# 服务端一致性巡检
fileupload scan

# 压测（50 个文件 × 100MB，16 并发）
fileupload bench --files 50 --size 100m --concurrency 16
```

### 子命令一览

| 子命令 | 功能 | 关键参数 |
|--------|------|---------|
| `upload` | 上传文件或目录 | `--concurrency`, `--compress`, `--chunk-size` |
| `download` | 下载文件或目录 | `-o`, `--range`, `--format` |
| `rm` | 删除文件或目录 | `--recursive` |
| `ls` | 列目录 | `--parent` |
| `stat` | 文件/目录详情 | — |
| `status` | 查询上传会话进度 | — |
| `scan` | 触发一致性巡检 | — |
| `bench` | 压测 | `--files`, `--size`, `--concurrency` |
| `config` | 查看当前配置 | — |
| `login` | JWT 登录 | `--username`, `--password` |
| `completion` | shell 补全脚本 | `bash \| zsh \| fish \| powershell` |

所有子命令支持 `--server`（默认 `http://localhost:8080`）和 `--namespace`（默认 `default`）。

### Tab 补全

```bash
source <(fileupload completion zsh)     # zsh
source <(fileupload completion bash)    # bash
fileupload completion fish | source     # fish
```

---

## API 参考

### tus 协议（可续传上传）

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/uploads` | 创建上传会话 |
| `HEAD` | `/uploads/{id}` | 查询上传进度 |
| `PATCH` | `/uploads/{id}` | 追加分片 |
| `DELETE` | `/uploads/{id}` | 取消上传 |

### REST API

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/v1/uploads/init` | 初始化上传会话 |
| `PUT` | `/v1/uploads/{id}/chunks/{index}` | 上传分片 |
| `GET` | `/v1/uploads/{id}/status` | 查询分片进度 |
| `POST` | `/v1/uploads/{id}/finalize` | 完成上传 |
| `HEAD` | `/v1/files?sha256={hex}&name={name}` | 秒传预检（内容去重） |
| `GET` | `/v1/files/{id}` | 下载文件（支持 Range） |
| `GET` | `/v1/dirs/{id}` | 下载目录（流式打包） |
| `POST` | `/v1/dirs` | 提交目录 manifest |
| `DELETE` | `/v1/files/{id}` | 删除文件或目录 |
| `GET` | `/v1/ls?parent={id}` | 列目录（含祖先链 breadcrumb） |
| `GET` | `/v1/stat/{id}` | 文件/目录信息 |
| `GET` | `/v1/preview/{id}` | 在线预览 |

### 鉴权

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/v1/auth/login` | JWT 登录 |
| `POST` | `/v1/auth/refresh` | 刷新 token |
| `GET` | `/v1/auth/me` | 当前用户信息 |

### 批量操作

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/v1/batch/delete` | 批量删除 |
| `POST` | `/v1/batch/download` | 批量打包下载 |
| `POST` | `/v1/batch/move` | 批量移动 |
| `POST` | `/v1/batch/copy` | 批量复制 |
| `POST` | `/v1/batch/tags` | 批量设置标签 |

### 分享

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/v1/share` | 创建分享链接 |
| `GET` | `/s/{token}` | 访问分享（重定向到下载） |

### 管理

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/v1/admin/status` | 系统状态 |
| `GET` | `/v1/admin/audit` | 审计日志（分页） |
| `POST` | `/v1/admin/scan` | 触发一致性巡检 |
| `GET` | `/health` | 健康检查 |
| `GET` / `POST` | `/ws` | WebSocket 实时推送 |

完整请求/响应详情见 [docs/api.md](docs/api.md)。

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

# Compose（含 Redis）
cd deploy/docker && docker compose up -d
```

### 生产建议

- **前端**放 Gateway/Nginx 做 TLS 终结、域名绑定
- **Redis**启用 AOF 持久化，配置密码
- **数据库**PostgreSQL 生产环境推荐，SQLite 适合开发/小规模
- **备份**定期备份数据目录 + SQLite 数据库文件
- **巡检**定期执行 `fileupload scan` 或配置定时任务
- **调优**根据机器配置调整 `worker_pool_size`、`write_timeout`

---

## 编译

```bash
make web        # 构建前端
make server     # 编译当前平台服务端
make cli        # 编译当前平台 CLI
make release    # 全平台交叉编译

# 产物在 build/ 目录
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
go test -race -count=1 ./...

# 带覆盖率和 race 检测
make test

# E2E 测试（全链路）
go test -v -run TestE2E ./internal/transport/
```

---

## 功能特性

| 特性 | 说明 |
|------|------|
| 双协议上传 | tus.io + REST，共享领域核心 |
| 动态分片 | 大文件自动切分，并发上传 |
| 客户端压缩 | zstd 压缩后传输，服务端透明解压 |
| 流式下载 | Range 分段 / 目录打包（tar.gz / zip） |
| 数据校验 | 分片级 + 整体 SHA-256 全链路防篡改 |
| 秒传 | 内容寻址去重，相同内容秒级完成 |
| 断点续传 | tus 原生 + CLI 状态文件 |
| 并发控制 | 全局 worker 池 |
| 命名空间隔离 | 多租户（Gateway 注入 X-Namespace） |
| 层级存储 | 目录上传后按原始层级路径存放 |
| 文件标签 | 关系型标签，批量标记 |
| 批量操作 | 删除 / 打包 / 移动 / 复制 / 标签 |
| 一致性巡检 | 孤儿文件、引用计数漂移检查 |
| 文件分享 | 密码保护、过期时间、下载次数限制 |
| JWT 鉴权 | 签发/验证/刷新/登录 API |
| Web UI | React + Ant Design 管理面板 |
| 文件预览 | 图片/文本/PDF/视频/音频在线预览 |
| WebSocket | 上传进度实时推送 |
| 审计日志 | 操作记录持久化、分页查询 |
| PostgreSQL | 生产级数据库支持，SQLite 双适配 |

---

## 项目结构

```
.
├── cmd/
│   ├── server/               # 服务端入口（HTTP 服务）
│   └── fileupload/           # CLI（Cobra 子命令）
├── internal/
│   ├── domain/               # 领域核心（模型、端口、上传/下载/批量编排）
│   ├── adapters/
│   │   ├── storage/          # 本地文件系统 + S3
│   │   ├── metadata/         # Redis 热数据 + SQLite/PostgreSQL 冷数据
│   │   ├── compressor/       # zstd / gzip / tar / zip
│   │   ├── hasher/           # SHA-256
│   │   └── auth/             # JWT 签发与验证
│   ├── transport/            # HTTP 路由 + 中间件 + WebSocket
│   ├── lifecycle/            # 会话清理 + 一致性巡检
│   └── config/               # YAML 配置 + 环境变量覆盖
├── web/                      # React 前端（Vite + Ant Design 5）
├── sdk/
│   ├── go/                   # Go SDK 独立包
│   └── README.md
├── deploy/
│   ├── docker/               # Dockerfile + docker-compose
│   └── systemd/              # systemd 单元文件
├── docs/
│   ├── adr/                  # 架构决策记录（5 份）
│   ├── api.md                # 完整 API 参考
│   ├── benchmark.md          # 压测数据
│   └── agents/               # Agent 配置
├── fileupload.yaml           # 默认配置
├── fileupload.yaml.example   # 配置模板
├── CONTEXT.md                # 领域词汇表
├── AGENTS.md                 # Agent 配置
└── Makefile
```

---

## 架构决策记录

| ADR | 决策 |
|-----|------|
| [0001](docs/adr/0001-physical-file-storage-strategy.md) | 物理文件存储策略 — Finalize 扁平写入 + SubmitDir 搬移到层级路径 |
| [0002](docs/adr/0002-file-tags-storage.md) | 文件标签存储方案 — file_tags 关联表 |
| [0003](docs/adr/0003-submitdir-reuse-finalize-records.md) | SubmitDir 复用 Finalize 记录 |
| [0004](docs/adr/0004-directory-upload-tree-construction.md) | 目录上传自动构建目录树 |
| [0005](docs/adr/0005-batch-download-zip-streaming.md) | 批量下载流式 zip 打包 |

---

## SDK

### Go SDK

```go
import "github.com/bilbilmyc/fileupload/sdk/go/fileupload"

client := fileupload.NewClient("http://localhost:8080")
info, err := client.Upload(ctx, "data.bin", fileupload.WithCompression("zstd"))
```

### TypeScript SDK

```typescript
import { FileuploadClient } from '@fileupload/sdk'

const client = new FileuploadClient({ endpoint: 'http://localhost:8080' })
const file = await client.upload(fileBlob, 'photo.jpg')
```

详见 [sdk/README.md](sdk/README.md)。

---

## 许可证

MIT
