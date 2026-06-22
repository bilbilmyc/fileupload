# fileupload 开发路线图

> 按阶段划分的完整开发计划，每个阶段完成后标记为 ✅

---

## Phase 1 — 功能优化 ✅

| # | 任务 | 状态 | 说明 |
|---|------|------|------|
| 1 | 后端搜索 API | ✅ | ListDir 支持 search 参数，SQLite LIKE 查询 |
| 2 | 上传分片失败重试 | ✅ | 指数退避 3 次（500ms/1s/2s） |
| 3 | 前端并发上传 | ✅ | 分批并发，支持 hardwareConcurrency |
| 4 | 批量下载流式化 | ✅ | GET 端点 + window.open 免内存 |
| 5 | React Error Boundary | ✅ | 三层包裹，错误提示+重试 |

---

## Phase 2 — 新功能 + 前端美化

| # | 任务 | 优先级 | 预计工时 | 说明 |
|---|------|--------|---------|------|
| 1 | 文件预览 | 🔴 P0 | 2-3d | 图片灯箱、文本/PDF在线查看、视频音频播放 |
| 2 | 暗色模式 | 🟡 P1 | 1d | Ant Design 5 暗色主题 + prefers-color-scheme |
| 3 | 移动端响应式 | 🟡 P1 | 2d | 小屏卡片列表、触摸友好、底部导航 |
| 4 | 文件图标系统 | 🟢 P2 | 0.5d | 按文件类型显示不同图标 |

### 详细说明

#### 1. 文件预览
- 后端新增 `GET /v1/preview/{id}` 端点，返回正确的 Content-Type
- 图片预览（缩略图 + 灯箱，基于 antd Image）
- 文本文件在线查看（自动检测编码）
- PDF 预览（基于浏览器原生 PDF 查看器或 iframe）
- 视频/音频播放（HTML5 video/audio）
- 大文件不做内存加载，使用 Range 请求流式读取

#### 2. 暗色模式
- 利用 Ant Design 5 `ConfigProvider theme={{ algorithm: theme.darkAlgorithm }}`
- 切换按钮放在 TopBar
- 检测 `prefers-color-scheme: dark` 自动启用
- 持久化选择到 localStorage

#### 3. 移动端响应式
- 文件列表在大屏用表格、小屏用卡片列表
- 批量操作栏固定在底部
- 上传面板在小屏折叠
- 触摸友好操作区域

#### 4. 文件图标系统
- 常见扩展名映射图标：📄 文档、🖼 图片、🎵 音频、🎬 视频、📦 压缩包、📊 表格、💻 代码
- 未知文件类型显示通用图标

---

## Phase 3 — 后端多功能支持

| # | 任务 | 优先级 | 说明 |
|---|------|--------|------|
| 1 | JWT 真实鉴权 | 🟡 P1 | 登录 API、JWT 签发/验证、namespace 从 token 解析 |
| 2 | 管理后台 API | 🟡 P1 | 系统监控、用户管理、审计日志持久化 |
| 3 | PostgreSQL 适配 | 🟡 P1 | 新增 postgres.go 适配器，支持共享元数据 |
| 4 | 存储配额管理 | 🟢 P2 | 按 namespace 设置存储上限 |
| 5 | 高级压缩策略 | 🟢 P2 | 图片 webp 转换、视频转码 |

---

## Phase 4 — 对外 SDK

| # | 任务 | 优先级 | 说明 |
|---|------|--------|------|
| 1 | Go SDK | 🟡 P1 | 独立包 `github.com/bilbilmyc/fileupload-sdk-go` |
| 2 | TypeScript SDK | 🟡 P1 | npm 包 `@fileupload/sdk` |
| 3 | SDK 文档+示例 | 🟢 P2 | 多语言 README、React Hook、Jupyter Notebook |

---

## Phase 5 — 长期规划

| # | 任务 | 优先级 | 说明 |
|---|------|--------|------|
| 1 | 文件分享（外链） | 🟡 P1 | 带过期/密码/次数限制的分享链接 |
| 2 | WebSocket 实时推送 | 🟡 P1 | 上传进度实时推送替代轮询 |
| 3 | Webhook 通知 | 🟢 P2 | 上传/删除事件触发 |
| 4 | 文件版本管理 | 🟢 P2 | 版本回滚、历史记录 |
| 5 | 存储冷热分层 | 🟢 P2 | 自动迁移到 S3 Glacier |
| 6 | Python SDK | 🟢 P2 | pip 包 `fileupload-sdk` |

---

## 已完成记录

- **2026-06-22**: Phase 1 功能优化全部完成
  - `git log --oneline` 查看最新提交
  - README 已覆盖全部功能

*最后更新: 2026-06-22*
