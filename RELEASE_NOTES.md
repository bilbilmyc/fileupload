# fileupload v0.14.0 — 发布说明

**稳定版本**：v0.14.0（2026-07-20）
**Git tag**：`v0.14.0`
**提交**：发布前最后一个 main 提交（见 GitHub Release）

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
- **运维加固**：关键操作审计日志、登录限流、安全响应头、控制面请求体限制
- **数据库迁移**：SQLite/PostgreSQL 版本化自动迁移，PostgreSQL 多实例 advisory lock
- **可重复压测**：固定随机种子、延迟分位数、吞吐/错误率门槛和自动清理

---

## 已验证（代码 + 测试可证）

| 维度 | 验证方式 | 数据 |
|---|---|---|
| Go 静态检查 | `go vet ./...` | 通过 |
| Go race 测试 | `go test -race -count=1 -timeout 120s ./...` | 全部 Go 包 PASS |
| 前端 lint | `pnpm web:lint` | 通过 |
| 前端单元测试 | `pnpm web:test` | **94 PASS + 3 skipped** |
| 前端生产构建 | `pnpm web:build` | TypeScript + Vite OK |
| E2E | Chromium smoke tests，单 worker | **10 PASS** |
| Go 漏洞扫描 | `govulncheck@v1.6.0 ./...` | 0 个可达漏洞 |
| 前端依赖审计 | `pnpm audit --audit-level high` | 未发现高危漏洞 |
| 数据库迁移 | SQLite 升级回归 + PostgreSQL CI 集成测试 | 版本表和索引校验 |
| 可重复压测 | Windows + SQLite + miniredis，50 × 1 MiB，8 并发 | 67.68 MiB/s，0% 错误，清理成功 |
| 构建产物 | tag CI | 6 平台 × server/cli = 12 个二进制 |

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

参考 `deploy/docker/`（Dockerfile + Docker Compose）和 `deploy/systemd/`：

```bash
make docker          # linux/amd64 镜像
make docker-arm64    # linux/arm64 镜像
```

**生产前必做**：

1. **HTTPS**：前置 nginx/caddy/cloudflare 终止 TLS，不要裸跑 :8080
2. **JWT 密钥**：设置 `AUTH_JWT_SECRET` 环境变量（≥32 字节随机）
3. **admin 密码**：使用配置中的 bcrypt 密码；禁止生产环境启用 `dev_admin_enabled`
4. **存储后端**：小规模使用 LocalFS；需要对象存储时按配置启用 S3，并验证权限和回收策略
5. **数据库**：SQLite 适合单机；多实例请用 PostgreSQL；升级前先备份并阅读 `docs/database-migrations.md`
6. **Redis**：生产启用认证和持久化；多实例部署需在网关增加分布式限流

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

| 限制 | 影响 | 处理方式 |
|---|---|---|
| **Postgres 集成测试本地依赖外部服务** | 本机无 PostgreSQL 时无法运行适配器集成测试 | CI 使用 PostgreSQL service 验证 |
| **进程内限流** | 多实例限流状态不共享 | 生产多实例需在网关/WAF 增加分布式限流 |
| **审计写入失败不阻断业务** | 极端情况下可能缺审计记录 | `slog` 告警；高要求场景接入外部可靠日志管道 |
| **SQLite 单写者模型** | 高并发写入扩展性有限 | 多实例/高并发使用 PostgreSQL |
| **i18n 无** | 错误消息、UI 文案均为中文 | 国际化时再做 |

---
## 稳定版之后不主动加新功能

本次为收尾版本。后续改动仅：

1. **生产部署发现的问题修复**（hotfix）
2. **用户明确提出的功能**
3. **安全审计发现的必修项**

如需新功能请开 issue 描述具体场景，不主动加新模块。

---

## 升级 / 回滚

### 升级路径

当前稳定版本以 GitHub Release 为准；本版本已补齐审计、安全加固、自动迁移和可重复压测工具。

- 旧版升级：先备份数据，再替换二进制并启动；服务会自动执行版本化迁移。
- 数据库 schema 变更：见 [docs/database-migrations.md](docs/database-migrations.md)

### 回滚

回滚前先停止服务并恢复与数据库一致的文件/数据库备份；迁移只向前兼容，不建议手工逆向 schema。详情见 `docs/database-migrations.md`。

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