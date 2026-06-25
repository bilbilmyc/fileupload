# fileupload v0.11.0 — 发布说明

**稳定版本**：v0.11.0（2026-06-25）
**Git tag**：`v0.11.0`
**提交**：`c069269`（main 分支）

---

## 这是什么

`fileupload` 是一个高性能、可自托管的文件上传下载服务：

- **分片上传**：tus 协议 + REST 双协议，单文件可分多片并行上传
- **秒传**：客户端先算 SHA-256，服务端命中已有 blob 则直接复用（ref_count +1）
- **目录上传**：浏览器选目录后批量上传，服务端构建目录树
- **流式打包下载**：批量文件实时打包为 zip / tar.gz / tar.zst，零临时文件
- **原子性目录删除**：中途失败可回滚（undo 操作栈，ADR-0006）
- **SDK 双端**：Go + JS/TS 完整覆盖 31 个端点
- **监控**：Prometheus 指标 + 6 告警规则 + 10 Grafana 面板 + Alertmanager 分流
- **管理面板**：单二进制内置 React SPA

---

## 已验证（代码 + 测试可证）

| 维度 | 验证方式 | 数据 |
|---|---|---|
| 后端编译 | `go build ./...` | 14 包 OK |
| 后端单元测试 | `go test ./...` | 14 包 PASS |
| 后端覆盖率 | `go test -cover` | **48.2%**（hasher 93% / lifecycle 94% / auth 88% / compressor 81%） |
| SDK 上传流 | httptest.Server | **44.2%**（11 个新测试覆盖 tus + REST） |
| 前端构建 | `npm run build` | TypeScript + Vite OK |
| 前端单元测试 | `vitest run` | **76 PASS + 3 skipped** |
| 前端覆盖率 | `vitest --coverage` | **53.18%**（sdk.ts 100% / upload-utils 95% / upload-task 100%） |
| E2E | Playwright | 3 smoke 测试 PASS |
| 监控配置 | alerts.yml + dashboard.json + alertmanager.yml | 各自有 YAML/JSON 解析测试 |
| 构建产物 | `make release` | 4 平台 × server/cli = 8 个二进制 |
| CI 流程 | `.github/workflows/ci.yml` | push / PR / tag 自动构建 |

---

## 部署方式

### 最小化部署（dev/test 环境）

```bash
# 1. 拉取 + 编译
git clone https://github.com/bilbilmyc/fileupload.git
cd fileupload
make release  # 产出 build/fileupload-server-linux-amd64

# 2. 准备配置
cp fileupload.yaml.example fileupload.yaml
# 编辑：改 storage.data_dir / redis.addr / db.type（sqlite | postgres）

# 3. 启动
./build/fileupload-server-linux-amd64
# 默认监听 :8080，前端 SPA 在 /

# 4. 验证
curl http://localhost:8080/health
curl http://localhost:8080/metrics | head
```

### 生产部署

参考 `deploy/docker/`（Dockerfile + docker-compose 待补）+ `deploy/systemd/`：

```bash
make docker          # linux/amd64 镜像
make docker-arm64    # linux/arm64 镜像
```

**生产前必做**：

1. **HTTPS**：前置 nginx/caddy/cloudflare 终止 TLS，不要裸跑 :8080
2. **JWT 密钥**：设置 `AUTH_JWT_SECRET` 环境变量（≥32 字节随机）
3. **admin 密码**：首次启动会在日志打印临时 admin 密码，**立即改**
4. **存储后端**：本地 FS 起步，S3 适配器已就绪（切换需改配置 + 重启）
5. **数据库**：SQLite 适合单机；多实例请用 Postgres（adapter 已实现）
6. **Redis**：可热数据用 Redis（hot/cold 架构），单实例模式也支持

### 监控接入

```yaml
# prometheus.yml scrape config
scrape_configs:
  - job_name: fileupload
    static_configs:
      - targets: ['fileupload-host:8080']

rule_files:
  - deploy/prometheus/alerts.yml

alerting:
  alertmanagers:
    - static_configs:
        - targets: ['alertmanager:9093']
```

Grafana：导入 `deploy/grafana/dashboard.json`（UID `fileupload-main`）

---

## 架构核心（4 ADR 决策）

- **ADR-0001**：Finalize 扁平路径 → SubmitDir 搬移到层级路径（运维可直读）
- **ADR-0002**：文件标签存 `file_tags` 关联表（关系型，非 JSON）
- **ADR-0003**：SubmitDir 复用 Finalize 的 FileMetadata 记录（避免重复 + ref_count 简单）
- **ADR-0004**：SubmitDir 自动创建中间目录节点（前端 webkitRelativePath 扁平 → 服务端建树）
- **ADR-0005**：批量下载 zip 流式打包（`archive/zip` 标准库，零临时文件）
- **ADR-0006**：BatchService.meta 用 `interface{FileStore; BlobStore}`（接口分离，消除类型断言 hack）
- **ADR-0007**：HierarchicalLayout 集中路径布局（Finalize 扁平 vs SubmitDir 层级两套格式收敛到一处）
- **ADR-0008**：IncrWithRollback helper（引用计数回滚闭包，幂等）

---

## 不能做的（已知限制）

| 限制 | 影响 | 何时修 |
|---|---|---|
| **未做真实负载测试** | 高并发下吞吐未知 | 上线前必须用 wrk/vegeta 实测 |
| **无安全审计** | JWT 强度、注入、SSRF 未系统审计 | 上线前请安全人员 review |
| **Postgres 集成测试仅 CI** | 本地不能跑 PG 适配器 | 已知限制，按用户指示未做 |
| **UploadContext 覆盖率 40%** | React hooks 状态机测试覆盖低 | 非阻塞，核心逻辑已抽纯函数 |
| **client.ts 8 个函数仍用 axios** | 双代码路径（SDK + axios） | v0.12.0 删除 axios 函数 |
| **无事务化 deleteDir** | 中途失败物理文件不回滚（DB 已回滚） | 评审遗留，需 SDK + adapter 改动 |
| **WebSocket 事件类型未文档化** | 客户端订阅事件类型靠反推 | docs/api.md 待补 |
| **i18n 无** | 错误消息、UI 文案均为中文 | 国际化时再做 |
| **SDK Go 上传流测试部分 skipped** | TestUpload / TestUploadDir 跳过（依赖 os.File） | 集成测试覆盖 |
| **无自动数据库 schema migration** | 新版本可能 break 旧 DB | 需 sql-migrate 或类似 |

---

## v0.11.0 之后不主动加新功能

本次为收尾版本。后续改动仅：

1. **生产部署发现的问题修复**（hotfix）
2. **用户明确提出的功能**
3. **安全审计发现的必修项**

如需新功能请开 issue 描述具体场景，不主动加新模块。

---

## 升级 / 回滚

### 升级路径

v0.11.0 是第一个稳定 API 集合（31 个端点 + 5 份 ADR + SDK 完整）。

- 旧版（v0.0.1 - v0.10.0）→ v0.11.0：二进制替换即可，配置文件向后兼容
- 数据库 schema 变更（如有）：见 `internal/adapters/metadata/*/migrate()`

### 回滚

`git checkout v0.X.Y` → `make release` → 替换部署。SQLite 数据库可直接回退，Postgres 需 `pg_dump` + 手动恢复。

---

## 联系方式 / 反馈

- GitHub Issues: https://github.com/bilbilmyc/fileupload/issues
- 项目 README: README.md
- 领域词汇: CONTEXT.md
- API 参考: docs/api.md
- CI 流程: docs/ci.md
- SDK 使用: sdk/USAGE.md
- 测试覆盖率: docs/coverage.md（Web）+ docs/go-coverage.md（Go）

---

## 致谢

v0.1.0 → v0.11.0 共 12 个 tag、约 30 个 commit，核心架构与代码质量由 Claude Fable 5 (Anthropic) 协助完成。但 **所有未经验证的领域（安全、负载、生产部署）需要人工评审**。