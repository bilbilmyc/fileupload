# CI/CD 工作流

本文档说明 fileupload 项目的持续集成与发布流程。所有配置在 `.github/workflows/ci.yml`。

## 触发矩阵

| 触发条件 | 跑的 jobs | 产物 |
|---|---|---|
| PR / push 到 main | test + build + docker | Docker 镜像（仅 push 时） |
| tag push (`v*`) | test + build + docker + **release** | GitHub Release + 8 个二进制 |
| 任意 main push | + **dev-release** | 持续 dev Release（每次 commit） |

## Jobs 详解

### 1. `test` — 单元测试 + vet

- 镜像：`ubuntu-latest`
- 服务依赖：Redis 7-alpine（`localhost:6379`）
- 命令：`go test -race -count=1 -timeout 120s ./...`
- 加 `go vet ./...`
- PR 时阻塞合并；push 时不影响后续 jobs（无依赖）

### 2. `build` — 8 平台交叉编译

`needs: [test]` — 测试失败则跳过构建。

矩阵 4 平台 × 2 组件（server / fileupload CLI）= 8 个二进制：

| 平台 | 组件 | 二进制 |
|---|---|---|
| linux/amd64 | server | `fileupload-server-linux-amd64` |
| linux/arm64 | server | `fileupload-server-linux-arm64` |
| darwin/amd64 | server | `fileupload-server-darwin-amd64` |
| darwin/arm64 | server | `fileupload-server-darwin-arm64` |
| linux/amd64 | cli | `fileupload-cli-linux-amd64` |
| linux/arm64 | cli | `fileupload-cli-linux-arm64` |
| darwin/amd64 | cli | `fileupload-cli-darwin-amd64` |
| darwin/arm64 | cli | `fileupload-cli-darwin-arm64` |

CGO 全关，`-ldflags="-s -w"` 剥离符号表。

每个二进制作为 artifact 上传，供后续 docker / release jobs 使用。

### 3. `docker` — 多架构镜像构建

`needs: [build]`。

- 使用 build job 预编译的二进制，无需 QEMU 模拟编译
- 注册表：`ghcr.io/${{ github.repository_owner }}/fileupload-server` 与 `fileupload-cli`
- tag 模式：
  - `semver:{{version}}` — v1.2.3
  - `semver:{{major}}.{{minor}}` — v1.2
  - `latest`（仅 main 分支）
  - `sha` 前缀（短 commit）

### 4. `dev-release` — 持续集成 Release

仅当 `github.ref == 'refs/heads/main'` 时触发。

- 下载所有 build artifacts
- 生成 `checksums.txt`
- 用 `softprops/action-gh-release` 创建 tag `dev` 的 GitHub Release
- target_commitish 指向当前 SHA

用途：每次 main 分支提交都有可下载的开发版本。

### 5. `release` — 正式版本发布

仅当 tag 匹配 `v*` 时触发。

- `needs: [build, docker]` — 等二进制与镜像都准备好
- 下载所有 artifacts → 生成 checksums → `softprops/action-gh-release` 创建正式 Release
- `generate_release_notes: true` 自动生成 release notes

## 本地复现 CI

### 跑测试（带 race）
```bash
make test
# 等价于：go test -race -count=1 ./...
```

### 跑完整 release（4 平台二进制）
```bash
make release
# 输出到 build/fileupload-{server,cli}-{linux,darwin}-{amd64,arm64}
```

### 跑 Docker 构建
```bash
# 单平台（amd64）
make docker
# 单平台（arm64）
make docker-arm64
```

### 打 tag 触发 CI release
```bash
git tag v0.5.0
git push origin v0.5.0
# CI 自动：build 8 个二进制 + docker 2 镜像 + 创建 GitHub Release
```

## 权限矩阵

| Job | permissions |
|---|---|
| (全局) | `contents: read`, `packages: write` |
| dev-release | + `contents: write`（创建 Release） |
| release | + `contents: write`（创建 Release） |

`packages: write` 用于推送 ghcr.io Docker 镜像。

## 故障排查

### CI 跑挂了？

1. **测试失败**：本地 `make test` 重现
2. **Docker 镜像缺失 `fileupload.yaml`**：检查 `deploy/docker/` 是否有正确的 Dockerfile
3. **二进制嵌入 dist 失败**：`make web` 先构建前端
4. **Release 不创建**：检查 tag 格式是否匹配 `v*`（v 开头）

### 本地发布调试

```bash
# 只跑某个目标
make server-linux-amd64

# 看完整 release 过程
make release 2>&1 | tail -20

# 清理重跑
make clean && make release
```

## 修改 workflow 后

修改 `.github/workflows/ci.yml` 后：
- 提交到 PR 分支即可触发 test + build + docker（不会触发 release）
- 合入 main 后会触发 dev-release
- 打新 tag 才会触发正式 release

## 相关文档

- `docs/api.md` — API 参考
- `sdk/USAGE.md` — SDK 使用指南
- `deploy/prometheus/alerts.yml` — Prometheus 告警
- `deploy/grafana/dashboard.json` — Grafana 仪表盘
- `CONTEXT.md` — 领域词汇表
- `docs/adr/` — 架构决策记录