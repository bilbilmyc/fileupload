# fileupload 开发路线图

> 按阶段划分的完整开发计划，每个阶段完成后标记为 ✅

---

## Phase 1 — 功能优化 ✅

| # | 任务 | 状态 | 变更文件数 | 说明 |
|---|------|------|-----------|------|
| 1 | 后端搜索 API | ✅ | 12 | ListDir 支持 search 参数，SQLite LIKE 查询 |
| 2 | 上传分片失败重试 | ✅ | 2 | 指数退避 3 次（500ms/1s/2s） |
| 3 | 前端并发上传 | ✅ | 1 | 分批并发，支持 hardwareConcurrency |
| 4 | 批量下载流式化 | ✅ | 4 | GET /v1/batch/download + window.open 免内存 |
| 5 | React Error Boundary | ✅ | 3 | 三层包裹，错误提示+重试 |

---

## Phase 2 — 新功能 + 前端美化 ✅

| # | 任务 | 状态 | 变更文件数 | 说明 |
|---|------|------|-----------|------|
| 6 | 文件预览 | ✅ | 6 | GET /v1/preview/{id} + 图片/文本/PDF/视频/音频 |
| 7 | 暗色模式 | ✅ | 6 | Ant Design 5 darkAlgorithm + prefers-color-scheme |
| 8 | 移动端响应式 | ✅ | 7 | Tailwind sm: 断点、批量工具栏图标化、灵活布局 |
| 9 | 文件图标系统 | ✅ | 1 | 12 类别扩展名映射图标+颜色 |

---

## Phase 3 — 后端多功能支持（进行中）

| # | 任务 | 状态 | 优先级 | 说明 |
|---|------|------|--------|------|
| 10 | JWT 真实鉴权 | ✅ | 🟡 P1 | 登录 API、JWT 签发/验证、namespace 从 token 解析 |
| 11 | 管理后台 API | ⏳ | 🟡 P1 | 系统监控、审计日志持久化 |
| 12 | PostgreSQL 适配 | ⏳ | 🟡 P1 | 新增 postgres.go 适配器 |
| 13 | 存储配额管理 | 📋 | 🟢 P2 | 按 namespace 设置存储上限 |
| 14 | 高级压缩策略 | 📋 | 🟢 P2 | 图片 webp 转换、视频转码 |

### 详细说明

#### 10. JWT 真实鉴权 ✅
- **领域层**: `internal/domain/auth.go` — AuthService 接口 + AuthClaims/TokenPair/LoginRequest 模型
- **适配层**: `internal/adapters/auth/jwt.go` — HS256 签发/验证、access+refresh token 对
  - 默认 admin 用户（admin / admin123，开发用）
  - 可扩展为从数据库或 LDAP 加载用户
- **传输层**: `internal/transport/auth.go` — 三个端点：
  - `POST /v1/auth/login` — 用户名密码登录
  - `POST /v1/auth/refresh` — 刷新 token
  - `GET /v1/auth/me` — 获取当前用户信息
- **中间件**: `JWTValidate` 从 `Authorization: Bearer <token>` 头解析 JWT，将 claims 和 namespace 注入 context
- **前端**: Login 页支持真实登录（用户名密码→JWT），可跳过进入演示模式
- **API 客户端**: 自动发送 `Authorization: Bearer` 头
- **配置**: `jwt_secret`（签名密钥）、`jwt_expiry`（过期小时数）

#### 11. 管理后台 API
- 系统状态 API `GET /v1/admin/status`：
  - 活跃上传会话数、worker 池状态
  - 磁盘用量（data dir + temp dir）
  - Redis / SQLite 连接状态
  - Goroutine 数、内存使用
- 审计日志：
  - 新增 `audit_log` SQLite 表（id, action, target_type, target_id, user_id, namespace, detail, created_at）
  - 批量操作、删除、登录等重要操作写入审计日志
  - `GET /v1/admin/audit?page=1&per_page=50` 分页查询
- 管理端点需 admin 角色 JWT

#### 12. PostgreSQL 适配
- 新增 `internal/adapters/metadata/postgres.go`
- 实现全部 `domain.Metadata` 接口方法
- 使用 `pgx` 驱动（或 `database/sql` + `lib/pq`）
- SQL 迁移表结构（与 SQLite 迁移对应）
- 递归查询（如目录树）使用 CTE `WITH RECURSIVE` 替代 Go 层递归
- 配置 `database.type: "postgres"` 时自动切换
- 注意：PostgreSQL 与 SQLite 在并发事务隔离级别上有差异

#### 13. 存储配额管理
- 新增 `storage_quotas` 表（namespace, max_bytes, used_bytes）
- 上传前检查配额，超出返回 413
- CLI `fileupload quota` 命令查看/设置配额
- 前端管理面板配额显示

#### 14. 高级压缩策略
- 图片上传后自动 webp 转换（服务端配置）
- 视频转码选项（预留）
- `upload.compress_strategy` 配置项

---

## Phase 4 — 对外 SDK

| # | 任务 | 状态 | 优先级 | 说明 |
|---|------|------|--------|------|
| 15 | Go SDK | 📋 | 🟡 P1 | 独立包 `github.com/bilbilmyc/fileupload-sdk-go` |
| 16 | TypeScript SDK | 📋 | 🟡 P1 | npm 包 `@fileupload/sdk` |
| 17 | SDK 文档+示例 | 📋 | 🟢 P2 | 多语言 README、React Hook、Jupyter Notebook |

### 详细说明

#### 15. Go SDK
- 从当前 CLI 客户端 `cmd/fileupload/client.go` 抽取公共接口
- 独立仓库 `github.com/bilbilmyc/fileupload-sdk-go`
- 核心类型：`Client`（配置 endpoint/token）、`File`、`Directory`
- 方法：`Upload` / `Download` / `Delete` / `List` / `Move` / `Copy` / `Tag` / `Stat`
- 支持选项模式：`WithToken()`、`WithNamespace()`、`WithCompression()`、`WithConcurrency()`
- 自动重试、进度回调、断点续传

#### 16. TypeScript SDK
- npm 包 `@fileupload/sdk`
- 基于现有 `api/client.ts` 抽取
- 浏览器 + Node.js 双环境支持
- 完整 TypeScript 类型定义
- React Hook 封装（`useFileUpload`、`useFileList`）

#### 17. SDK 文档+示例
- 多语言 README（中/英）
- Go SDK 示例：上传/下载/目录操作
- JS SDK 示例：React 组件 + Node.js CLI
- Python 示例（Jupyter Notebook）

---

## Phase 5 — 长期规划

| # | 任务 | 状态 | 优先级 | 说明 |
|---|------|------|--------|------|
| 18 | 文件分享（外链） | 📋 | 🟡 P1 | 带过期/密码/次数限制的分享链接 |
| 19 | WebSocket 实时推送 | 📋 | 🟡 P1 | 上传进度实时推送替代轮询 |
| 20 | Webhook 通知 | 📋 | 🟢 P2 | 上传/删除事件触发 HTTP 回调 |
| 21 | 文件版本管理 | 📋 | 🟢 P2 | 版本回滚、历史记录 |
| 22 | 存储冷热分层 | 📋 | 🟢 P2 | 自动迁移到 S3 Glacier |
| 23 | Python SDK | 📋 | 🟢 P2 | pip 包 `fileupload-sdk` |
| 24 | 国际化 (i18n) | 📋 | 🟢 P2 | react-i18next 中英文切换 |
| 25 | 文件搜索增强 | 📋 | 🟢 P2 | 全文搜索、标签搜索、高级过滤 |
| 26 | OAuth2 / SSO 登录 | 📋 | 🟢 P2 | GitHub / Google / LDAP 登录 |

### 详细说明

#### 18. 文件分享（外链）
- 新增 `shares` 表（token, file_id, password_hash, expires_at, max_downloads, current_downloads）
- `POST /v1/share` 创建分享链接
- `GET /s/{token}` 访问分享（可加密码验证）
- 前端：分享按钮 + 复制链接 + 密码设置

#### 19. WebSocket 实时推送
- `GET /ws` WebSocket 端点
- 上传进度、操作完成、错误事件实时推送
- 前端使用 `useWebSocket` hook 替代轮询

#### 20. Webhook 通知
- 新增 `webhooks` 表（id, url, events, secret, namespace）
- 上传完成、删除、分享访问等事件触发
- 支持 HMAC 签名验证

#### 21. 文件版本管理
- 新增 `file_versions` 表追踪文件修改历史
- 保留 N 个版本（可配置）
- `GET /v1/files/{id}/versions` 版本列表
- `POST /v1/files/{id}/restore/{version}` 版本回滚

#### 22. 存储冷热分层
- 配置规则自动迁移旧文件到 S3 Glacier / Deep Archive
- 文件状态标记：hot / cold / restoring
- 冷文件访问时自动发起恢复请求

#### 23. Python SDK
- pip 包 `fileupload-sdk`
- 面向数据科学/ML 工作流
- Jupyter Notebook 示例

#### 24. 国际化 (i18n)
- react-i18next 集成
- 中/英文语言包
- 语言选择持久化

#### 25. 文件搜索增强
- 后端全文搜索（SQLite FTS5）
- 标签组合过滤
- 文件类型/大小/日期范围过滤

#### 26. OAuth2 / SSO 登录
- GitHub OAuth App 登录
- Google OAuth 2.0
- LDAP / Active Directory
- 与现有 JWT 体系兼容

---

## 已完成记录

- **2026-06-22 15:00**: Phase 1 功能优化全部完成
  - `git log --oneline` 查看最新提交
- **2026-06-22 16:30**: Phase 2 新功能 + 前端美化全部完成
  - 提交: `ae2e0da` — "feat: Phase 1+2 complete"
- **2026-06-22 18:00**: Phase 3 Task 10 JWT 鉴权完成
  - 分支: `phase3-backend-auth`
  - 提交: `ca0f47c` — "feat: JWT 鉴权 + 登录 API + 前端接入"

*最后更新: 2026-06-22*
