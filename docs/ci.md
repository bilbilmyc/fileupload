# fileupload CI/CD 工作流

本文档说明 fileupload 项目的持续集成与发布流程，配置位于 `.github/workflows/ci.yml`。

## 触发矩阵

| 触发条件 | jobs | 产物 |
|---|---|---|
| Pull Request | `test` + `web` + `build` | 8 个跨平台二进制 artifacts |
| push 到 `main` | 上述 jobs + `docker` + `dev-release` | GHCR server 镜像 + 持续开发版 Release |
| tag push（`v*`） | 上述 jobs + `docker` + `release` | 版本 server 镜像 + GitHub Release |
| 手动运行 | 按选择的 workflow 事件执行 | 依触发 ref 决定是否发布 |

所有 job 默认仅拥有 `contents: read`；只有 Docker job 获得 `packages: write`，只有 Release job 获得 `contents: write`。

## Jobs 详解

### 1. `test` — 后端质量门禁

- Redis 7-alpine 服务运行在 `localhost:6379`
- 执行 `go test -race -count=1 -timeout 120s ./...`
- 执行 `go vet ./...`
- 失败后不会进入跨平台构建

### 2. `web` — 前端生产构建

- 使用 Node.js 20
- 使用 `npm ci`，严格按照 `web/package-lock.json` 安装依赖
- 执行 `npm run build`，确保 TypeScript 与 Vite 生产构建可用

### 3. `build` — 8 个跨平台二进制

`needs: [test, web]`。矩阵为 Linux/macOS × amd64/arm64 × server/CLI：

| 平台 | 组件 | 二进制 |
|---|---|---|
| linux/amd64 | server | `fileupload-server-linux-amd64` |
| linux/arm64 | server | `fileupload-server-linux-arm64` |
| darwin/amd64 | server | `fileupload-server-darwin-amd64` |
| darwin/arm64 | server | `fileupload-server-darwin-arm64` |
| linux/amd64 | CLI | `fileupload-cli-linux-amd64` |
| linux/arm64 | CLI | `fileupload-cli-linux-arm64` |
| darwin/amd64 | CLI | `fileupload-cli-darwin-amd64` |
| darwin/arm64 | CLI | `fileupload-cli-darwin-arm64` |

编译关闭 CGO，使用 `-trimpath -ldflags="-s -w"` 减少产物体积。Artifacts 保留 14 天。

### 4. `docker` — server 镜像构建与推送

仅在 `main` push 或 `v*` tag push 时运行，使用 `build` job 的预编译 server 二进制，不需要 QEMU 模拟 Go 编译。

- 注册表：`ghcr.io/${{ github.repository_owner }}/fileupload-server`
- 架构：`linux/amd64`、`linux/arm64`
- main 推送生成 `latest` 与 commit SHA tag
- 版本 tag 生成 semver tag、`latest`（仅默认分支）与 commit SHA tag
- 开启 BuildKit GitHub Container Registry 缓存、 provenance 与 SBOM
- CLI 仅作为 GitHub Release 二进制发布，不制作没有运行时意义的 CLI 镜像

### 5. `dev-release` — main 持续 Release

仅当 `github.ref == 'refs/heads/main'` 时触发，下载 8 个二进制并生成 `checksums.txt`，更新 `dev` Release。

### 6. `release` — 正式版本发布

仅当 tag 匹配 `v*`，且所有质量门禁、构建和 server 镜像发布成功后触发。下载 8 个二进制，生成 `checksums.txt`，再创建 GitHub Release 并自动生成 release notes。

## 本地复现构建

```bash
# 构建前端
cd web && npm ci && npm run build

# 构建多平台二进制
make release

# 构建完整 server Docker 镜像
make docker
```

## 发布版本

```bash
git tag v0.5.0
git push origin v0.5.0
```

CI 会执行质量门禁、构建 8 个二进制、发布两个架构的 server 镜像，并创建正式 GitHub Release。

## 修改 workflow 后

- PR 会执行后端质量检查、前端生产构建和跨平台编译
- 合入 `main` 后额外发布 `latest` server 镜像与 `dev` Release
- 推送 `v*` tag 后发布 semver server 镜像与正式 Release
