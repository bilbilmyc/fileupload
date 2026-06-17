# fileupload — 文件上传下载服务

[![Go](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

高性能、可自托管的文件上传下载服务。支持动态分片、客户端压缩、流式打包下载、SHA-256 数据校验、断点续传、秒传（内容寻址去重）。用 Go 编写，单二进制部署。

---

## 功能特性

| 特性 | 说明 |
|------|------|
| **双协议上传** | tus.io 协议 + 自定义 REST API |
| **动态分片** | 大文件自动切分，并发上传 |
| **客户端压缩** | zstd 压缩后传输，服务端透明解压 |
| **流式下载** | 单文件 Range 分段下载，目录 tar.gz/tar.zst 流式打包 |
| **数据校验** | 分片级 + 整体级 SHA-256，传输与存储全链路防篡改 |
| **秒传** | 内容寻址去重（content-addressed storage），相同文件秒级完成 |
| **断点续传** | tus 协议原生 + REST 自实现 |
| **并发控制** | 全局 worker 池，限制并发磁盘 IO |
| **命名空间** | 多租户隔离（由上游 Gateway 注入） |
| **一致性巡检** | 定时/手动扫描孤儿文件、引用计数漂移 |
| **Go CLI** | 完整命令行客户端（上传/下载/管理/压测） |
| **Web UI** | 自带浏览器测试面板（上传/下载/文件列表） |

## 快速开始

### 前提条件

- Go 1.23+
- Redis（上传会话热数据）
- （可选）Docker Compose

### 从源码运行

```bash
# 克隆项目
git clone https://github.com/mayc/casdao/fileupload.git
cd fileupload

# 确保 Redis 运行在 localhost:6379

# 启动服务端
go run ./cmd/server

# 另开终端 — 上传文件
go run ./cmd/fileupload upload README.md

# 下载文件
go run ./cmd/fileupload download <fileID> -o output.md

# 打开浏览器测试面板
open http://localhost:8080
```

### 使用 Docker Compose

```bash
cd deploy/docker
docker compose up -d
# 服务端运行在 http://localhost:8080
```

## 配置

配置文件为 YAML 格式（默认读取当前目录下的 `fileupload.yaml`），或通过环境变量覆盖。

### 完整配置项

```yaml
server:
  addr: ":8080"              # 监听地址
  read_timeout: 30           # 读取超时（秒）
  write_timeout: 300         # 写入超时（秒）
  idle_timeout: 60           # 空闲超时（秒）

storage:
  type: "local"              # 存储类型
  data_dir: "data"           # 数据目录
  temp_dir: "tmp"            # 临时分片目录

redis:
  addr: "localhost:6379"     # Redis 地址
  password: ""               # 密码
  db: 0                      # 数据库编号
  prefix: "upload:"          # key 前缀

database:
  type: "sqlite"             # 数据库类型
  path: "fileupload.db"      # SQLite 文件路径

upload:
  session_ttl_minutes: 60    # 会话超时（分钟）
  default_chunk_size: 10485760  # 分片大小（字节）
  worker_pool_size: 4        # worker 池大小
  worker_queue_size: 100     # 排队上限

download:
  max_archive_size: 0        # 打包上限（0=不限）
```

### 环境变量覆盖

```bash
FILEUPLOAD_CONFIG=/etc/fileupload/config.yaml
FILEUPLOAD_REDIS_ADDR=redis.example.com:6379
FILEUPLOAD_DB_PATH=/data/fileupload.db
FILEUPLOAD_STORAGE_DATA_DIR=/data/files
FILEUPLOAD_CHUNK_SIZE=20971520
```

## API 参考

### tus 协议 (可续传上传)

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/uploads` | 创建上传会话 |
| `HEAD` | `/uploads/{id}` | 查询上传进度（Upload-Offset） |
| `PATCH` | `/uploads/{id}` | 追加分片 |
| `DELETE` | `/uploads/{id}` | 取消上传 |

### REST API

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/v1/uploads/init` | 创建上传会话 |
| `PUT` | `/v1/uploads/{id}/chunks/{index}` | 上传分片 |
| `GET` | `/v1/uploads/{id}/status` | 查询分片进度 |
| `POST` | `/v1/uploads/{id}/finalize` | 完成上传（合并+校验） |
| `HEAD` | `/v1/files?sha256={hex}` | 秒传预检 |
| `GET` | `/v1/files/{id}` | 下载文件（支持 Range） |
| `GET` | `/v1/dirs/{id}` | 下载目录（流式打包） |
| `POST` | `/v1/dirs` | 提交目录 manifest |
| `DELETE` | `/v1/files/{id}` | 删除文件 |
| `GET` | `/v1/ls` | 列目录 |
| `GET` | `/v1/stat/{id}` | 文件信息 |
| `POST` | `/v1/admin/scan` | 触发一致性巡检 |
| `GET` | `/health` | 健康检查 |

### 错误码

| HTTP 状态码 | 含义 |
|-------------|------|
| 460 | 分片 SHA-256 校验失败 |
| 422 | 整体 SHA-256 校验失败 |
| 503 | 服务忙（worker 池满） |
| 410 | 文件已损坏 |

## 编译

### 当前平台

```bash
make server      # 编译服务端
make cli         # 编译 CLI
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

## 部署

### systemd （Linux）

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
- Redis 启用 AOF 持久化
- 定期执行 `fileupload scan` 或开启自动巡检
- 数据目录和 SQLite 文件定期备份

## CLI 使用

```bash
# 上传
fileupload upload large-file.dat --concurrency 8 --compress zstd

# 上传目录
fileupload upload ./my-dir/

# 下载
fileupload download abc123 -o output.bin

# 目录打包下载
fileupload download dir_abc -o project.tar.gz --format tar.gz

# 分片下载（断点续传）
fileupload download abc123 -o partial.bin --range 0-1048575

# 文件信息
fileupload stat abc123

# 列目录
fileupload ls /

# 删除
fileupload rm abc123

# 压测
fileupload bench --files 50 --size 100m --concurrency 16

# 触发服务端巡检
fileupload scan
```

## 架构概览

```
客户端 (CLI / Web / SDK)
      │ HTTP (tus + REST)
┌─────▼─────────────────────┐
│ 传输层 (net/http + chi)   │
│ tus Handler | REST Handler│
│ 下载 Handler | 中间件     │
└─────┬─────────────────────┘
      ▼
┌──────────────────────────┐
│ 领域核心                  │
│ UploadService / Download │
│  WorkerPool              │
└─────┬─────────────────────┘
      │ 端口接口
┌─────▼─────────────────────┐
│ 适配层（可插拔）           │
│ Storage  Metadata         │
│ Compressor  Hasher        │
└─────┬─────────────────────┘
      │
  ┌───┴───┬──────────┐
  │ 磁盘  │ Redis   │ SQLite
```

详见 [docs/architecture-fileupload-2026-06-17.md](docs/architecture-fileupload-2026-06-17.md)

## 项目结构

```
.
├── cmd/
│   ├── server/               # 服务端入口
│   └── fileupload/           # CLI 客户端（9 个子命令）
├── internal/
│   ├── domain/               # 领域核心（模型/端口/服务）
│   ├── adapters/             # 适配层实现
│   │   ├── storage/          # 本地文件系统
│   │   ├── metadata/         # Redis + SQLite 门面
│   │   ├── compressor/       # zstd/gzip/tar
│   │   └── hasher/           # SHA-256
│   ├── transport/            # HTTP 传输层
│   │   ├── router.go         # 路由 + 静态文件
│   │   ├── tus.go            # tus/REST/下载 handler
│   │   ├── middleware.go     # 中间件
│   │   └── static/           # 前端测试页面
│   ├── lifecycle/            # 会话清理 + 一致性巡检
│   └── config/               # 配置加载（YAML）
├── deploy/
│   ├── docker/               # Dockerfile + Compose
│   └── systemd/              # systemd 服务单元
├── docs/                     # 架构文档与设计 spec
├── Makefile
├── fileupload.yaml           # 默认配置文件
├── go.mod / go.sum
└── README.md
```

## 开发

```bash
# 代码检查
make vet
make lint

# 测试
make test

# 构建
make
```

## License

MIT
