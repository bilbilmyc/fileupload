# Changelog

## [v0.14.1] - 2026-07-20

### Changed

- 追平已通过 CI 的依赖维护更新：`actions/download-artifact` v8、`golang.org/x/crypto` v0.54.0、`github.com/redis/go-redis/v9` v9.21.0、`golang.org/x/sync` v0.22.0。
- 稳定版发布基于最新 main 提交，并复用 v0.14.0 的完整功能与安全加固。

### Verification

- main 分支最新 CI 已通过安全扫描、race 测试、前端测试、浏览器 smoke tests、跨平台构建和 Docker 镜像构建。

## [v0.14.0] - 2026-07-20

### Added

- 关键写操作、认证/授权失败、限流和管理操作审计日志，并新增管理员审计查询说明
- SQLite/PostgreSQL 版本化自动数据库迁移，支持旧数据库升级和 PostgreSQL 多实例迁移锁
- `fileupload bench` 可重复压测：固定种子、延迟 p50/p95/p99、吞吐与错误率门槛、自动清理和 JSON 输出
- 安全审查记录、数据库迁移说明和本机可复现压测基线文档
- CI `govulncheck` 与前端高危依赖审计门禁

### Security

- 增加登录失败限流与未知用户 dummy bcrypt 校验，降低暴力破解和用户名枚举风险
- 增加 CSP、HSTS（TLS）、防点击劫持、防 MIME 嗅探等安全响应头
- 限制控制面请求体大小，避免非上传端点被异常大请求占用资源

### Verification

- Go `vet`、`go test -race` 全量通过
- 前端 lint、94 个单元测试、生产构建和 10 个 Chromium smoke tests 通过
- `govulncheck@v1.6.0` 无可达漏洞；`pnpm audit --audit-level high` 通过
- Windows + SQLite + miniredis 本地基线：50 × 1 MiB、8 并发、50 MiB、67.68 MiB/s、0% 错误、清理成功


fileupload 项目所有重要变更记录。格式基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)。

## [v0.1.0] - 2026-06-24

### 重大变更（架构重构）

按 2026-06-24 架构评审报告系统落地 5 个深化候选。

### Added（新增）

- **ADR-0006 / 0007** — 新增 2 份架构决策记录，固化 BatchService 接口分离与路径布局约定
- **`internal/domain/layout.go`** — `HierarchicalLayout` 值类型，统一 Finalize 扁平路径与 SubmitDir 层级路径的格式化
- **`cmd/server/builder.go`** — `Build(deps Deps) (*Server, error)` 工厂 + `Server` 句柄，把装配图从 main() 抽出
- **端口 `HealthCheck`** — `domain.Storage` 与 `domain.Metadata` 新增 `HealthCheck(ctx) error` 方法
- **`domain.Metadata` 复合接口纳入 `ShareStore`** — 统一元数据面
- **Compressor codec 注册表** — 加新格式（bzip2 / 7z 等）= 新增 `codec_xxx.go` + `init()` 注册一行

### Changed（变更）

- **ADR-0001 路径约定落地** — `upload.go:350` / `:470` 的 `fmt.Sprintf` 内联路径格式收敛到 `HierarchicalLayout.FlatPath` / `HierarchicalPath` / `Move`
- **`BatchService.meta` 字段类型** — 从 `FileStore` 升格为 `interface{ FileStore; BlobStore }`（接口分离，ADR-0006）
- **端口文件拆分** — `internal/domain/ports.go` (133 行) 拆为 `ports_storage.go` / `ports_metadata.go` / `ports_compressor.go` / `ports_hasher.go` 四个文件
- **`Compressor` 调度器** — 原本的两个 switch + 未使用的 `zstdDec/zstdEnc` 字段，改为包级 codec 注册表
- **`serverHealth`** — 不再握 `*redis.Client`，改走 `Storage.HealthCheck` / `Metadata.HealthCheck`
- **`cmd/server/main.go`** — 从 264 行（含 17 个 `log.Fatalf`）缩到 ~85 行
- **提交信息统一中文**（延续既有约定）

### Fixed（修复）

- **引用计数静默吞错（Content-Addressed Storage 不变量被破坏）**
  - `BatchService.IncrBlobRefIfAvailable` 通过类型断言 + `_ =` 吞错，导致 BatchCopy / copyDirChildren 在 IncrBlobRef 失败时产生"文件记录存在但 ref_count 未增"的悬挂记录。修复后调用 `s.meta.IncrBlobRef` 直接传播错误
- **SubmitDir 物理文件搬移静默吞错** — `upload.go:472-479` 的 `Open/Write/Delete` 三步任一失败均被吞掉，导致 `ContentBlob.StoragePath` 与实际文件位置漂移。修复后改走 `HierarchicalLayout.Move()`，失败时 log + continue，运维可见

### Tests（测试）

- **`TestBatchService_BatchCopy_IncrementsRefCount`** — 回归测试：BatchCopy 后 `blob.RefCount` 从 1 增到 2
- **`TestBatchService_BatchCopyDir_IncrementsRefCount`** — 回归测试：递归目录复制同样保证引用计数 +1
- 修复前两个测试均失败；修复后通过

### 验证

- `go build ./...` — exit 0
- `go test ./...` — 12 个包全部 ok
- `go vet ./...` — exit 0
- `make release` — 4 平台 × server/cli = 8 个二进制（linux/darwin × amd64/arm64）

### Files Changed

```
docs/adr/0006-batchservice-meta-interface-segregation.md (new)
docs/adr/0007-hierarchical-layout.md (new)
cmd/server/builder.go (new)
cmd/server/main.go (264 → 85 lines)
internal/domain/layout.go (new)
internal/domain/ports_storage.go (new)
internal/domain/ports_metadata.go (new)
internal/domain/ports_compressor.go (new)
internal/domain/ports_hasher.go (new)
internal/domain/ports.go (deleted)
internal/domain/batch.go (interface separation)
internal/domain/upload.go (use HierarchicalLayout)
internal/adapters/compressor/compressor.go (registry)
internal/adapters/compressor/codec_none.go (new)
internal/adapters/compressor/codec_gzip.go (new)
internal/adapters/compressor/codec_zstd.go (new)
internal/adapters/compressor/codec_zip.go (new)
internal/adapters/storage/localfs.go (HealthCheck)
internal/adapters/storage/s3.go (HealthCheck)
internal/adapters/metadata/{facade,sqlite,postgres,redis}.go (HealthCheck)
internal/domain/batch_test.go (T2 regression tests)
```

## [v0.0.1] - prior

初版发布。