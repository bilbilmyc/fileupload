# fileupload CI/CD 工作流

本文档说明项目的持续集成、依赖检查和发布流程。核心配置：

- `.github/workflows/ci.yml`：质量门禁、构建、镜像与 Release
- `.github/workflows/dependency-review.yml`：Pull Request 依赖风险检查
- `.github/dependabot.yml`：GitHub Actions、Go、pnpm 与 Docker 依赖更新

## 触发矩阵

| 触发条件 | 质量门禁 | 发布产物 |
|---|---|---|
| Pull Request → `main` | Backend、Web、Chromium E2E、12 个跨平台构建、Dependency Review | 临时 Actions artifacts |
| push → `main` | 同上 | GHCR 多架构 `latest`/SHA 镜像 + `dev` prerelease |
| tag push（`v*`） | 同上 | GHCR semver 多架构镜像 + 正式 GitHub Release |
| 手动运行 | 执行质量门禁与构建 | 不自动发布 |

所有 job 默认只有 `contents: read`。Docker 发布 job 单独声明 `packages: write`，Release job 单独声明 `contents: write`。

## Jobs

### `backend` — Go 质量门禁

- 放置 `web/dist` 占位产物（`web/embed.go` 内嵌前端，Go 编译前必须存在该目录）
- `gofmt` 零差异检查
- `go mod tidy` 后验证 `go.mod` / `go.sum` 没有变化
- `go vet ./...`
- `go test -race -count=1 -timeout 120s ./...`
- 额外生成 atomic coverage，并上传 `go-coverage` artifact
- Redis 7 作为测试 service

> `web/dist` 不纳入版本控制。质量门禁只需能通过编译，因此放置占位 `index.html` 与 `assets/*` 即可；真实前端由 `build` job 注入（见下）。

### `web` — 前端质量与生产构建

- Node.js 24、pnpm 11.12.0
- 根目录执行 `pnpm install --frozen-lockfile`
- ESLint 零 warning
- Vitest coverage
- TypeScript + Vite production build
- 上传 `web-dist` 与 `web-coverage` artifacts

### `e2e` — 浏览器烟雾测试

- 安装 Playwright Chromium
- Playwright 自动启动 Vite，不要求开发者预先启动服务
- 登录接口按测试场景 mock，避免 CI 依赖固定管理员密码或常驻后端
- 无论成功失败都尝试上传 HTML report 与 test results

### `build` — 12 个跨平台二进制

矩阵为 Linux、macOS、Windows × amd64/arm64 × server/CLI：

| 平台 | server | CLI |
|---|---|---|
| linux/amd64 | `fileupload-server-linux-amd64` | `fileupload-cli-linux-amd64` |
| linux/arm64 | `fileupload-server-linux-arm64` | `fileupload-cli-linux-arm64` |
| darwin/amd64 | `fileupload-server-darwin-amd64` | `fileupload-cli-darwin-amd64` |
| darwin/arm64 | `fileupload-server-darwin-arm64` | `fileupload-cli-darwin-arm64` |
| windows/amd64 | `fileupload-server-windows-amd64.exe` | `fileupload-cli-windows-amd64.exe` |
| windows/arm64 | `fileupload-server-windows-arm64.exe` | `fileupload-cli-windows-arm64.exe` |

构建关闭 CGO 并启用 `-trimpath`。版本、commit 和 UTC 构建时间通过 Go linker flags 注入；CLI 可运行 `fileupload --version` 查看。

server 组件在编译前会下载 `web` job 产出的 `web-dist` artifact 到 `web/dist`，使 `web/embed.go` 内嵌**真实的管理面板**而非占位页；CLI 不引用 `web` 包，无需下载。因此 `build` 依赖 `web` job 完成。

### `quality-gate` — 稳定的分支保护检查

`CI success` 汇总 backend、web、e2e 和 build 的结果。建议在 GitHub Branch protection / Ruleset 中把 **CI success** 和 **Dependency review** 配为必需检查，避免矩阵名称变化导致保护规则频繁调整。

### `docker` — GHCR 多架构镜像

仅在 `main` 或 `v*` tag push 时执行：

1. 下载 linux/amd64 与 linux/arm64 的预编译 server；
2. 使用一个 Buildx build 生成 `linux/amd64,linux/arm64` manifest；
3. 推送至 `ghcr.io/<owner>/fileupload-server`；
4. 生成 provenance、SBOM，并使用 GitHub Actions cache。

单次多平台构建避免两个架构并发覆盖同一 Docker tag。

### `dev-release` 与 `release`

- `main`：更新 `dev` prerelease，且不会设置为 latest release。
- `v*` tag：生成正式 Release 与自动 release notes。
- 两类 Release 都包含 12 个二进制和 `checksums.txt`。
- Release 只会在质量门禁和 Docker 镜像发布都成功后执行。

## 依赖安全与自动更新

- Pull Request 会运行 `actions/dependency-review-action`，高危及以上新增依赖会失败。
- Dependabot 每周一（Asia/Shanghai）依次检查 GitHub Actions、Go modules、pnpm workspace 和 Dockerfile。
- pnpm workspace 在 Dependabot 中使用 `npm` package ecosystem，这是 GitHub 对 npm/yarn/pnpm JavaScript 包管理器的统一配置入口。

## 本地复现

```bash
# 安装锁定依赖
pnpm install --frozen-lockfile

# 前端静态检查、覆盖率、构建
pnpm web:lint
pnpm web:test:coverage
pnpm web:build

# 浏览器烟雾测试（首次先安装 Chromium）
pnpm --dir web exec playwright install chromium
pnpm web:e2e

# Go 门禁
gofmt -w ./cmd ./internal
go mod tidy
go vet ./...
go test -race -count=1 ./...

# Unix / WSL / Git Bash 下可直接运行聚合目标
make check
make release
```

## 发布版本

```bash
git tag v0.5.0
git push origin v0.5.0
```

Tag workflow 会先跑完整质量门禁，再构建 12 个二进制、发布两个 CPU 架构共用的 server manifest，最后创建 GitHub Release。
