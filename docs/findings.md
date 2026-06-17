# 发现与决策

## 需求
- 文件上传下载服务，支持文件 + 目录
- 业界常见功能：切片、压缩、数据校验、并发线程
- 支持断点续传、秒传（去重）
- 首批：服务端 + Go CLI；后续：Web UI、多语言 SDK

## 研究发现

### 项目现状（2026-06-17 探查）
- `/Users/mayc/work/casdao/fileupload` 目录基本为空，仅有 `.claude/settings.local.json`
- 非 git 仓库（`git rev-parse` 返回 fatal: not a git repository）
- 全新项目，无历史代码约束

### 选定的架构（方案 A：分层 + 共享 UploadService 核心）
```
客户端（CLI/Web/SDK）
    │ HTTP（tus + REST）
传输层 Transport（net/http + chi）
    │  tus Handler | REST Handler | 下载 Handler
    ▼
领域核心 Domain Core
    UploadService（会话/秒传/并发分片合并/解压/校验）
    DownloadService（流式打包/校验和返回）
    ▼ 端口/接口
适配层 Adapters（可插拔）
    Storage（本地FS→S3） | Metadata（Redis热+DB冷：SQLite/PG） | Compressor（gzip/zstd/tar） | Hasher（SHA-256）
```

### 三层职责
- **传输层**：HTTP 协议解析、路由、限流；把请求翻译成对核心的调用。不含业务。
  - tus Handler：POST 创建 / PATCH 追加分片 / HEAD 查询偏移
  - REST Handler：自定义 init/chunk/merge 接口
  - 下载 Handler：流式打包响应
- **领域核心**：业务大脑。UploadService 编排上传会话/秒传/并发分片合并/上传后解压/整体校验；DownloadService 编排打包下载与校验和返回。两协议共享同一核心（避免逻辑重复）。
- **适配层**：四个端口各定义接口、实现可插拔，满足「本地可插 S3」「SQLite+PG 可插拔」「Redis 热 + DB 冷」。

## 技术决策
| 决策 | 理由 |
|------|------|
| Go + net/http + chi | 高并发/流式/切片是 Go 强项；轻量依赖、可控 |
| 本地 FS 起步 + Storage 端口 | 起步最简，端口抽象为未来 S3 留口 |
| tus + REST 双协议 | tus 生态成熟可断点续传；REST 补自定义场景；共享核心避免重复 |
| Redis 热数据 + DB 冷数据 | 热数据（进行中会话/分片状态）短生命周期放 Redis；已完成文件信息放 DB |
| SQLite + PG 可插拔 | 起步零运维 SQLite，端口抽象为 PG 多节点扩展留口 |
| 动态分片大小 | 小文件 1MB / 大文件 10MB+，tus 支持，平衡片数与合并开销 |
| 客户端压缩 + 服务端解压 + 下载打包 | 上传省带宽、存储为原始数据；下载目录打包成 .tar.gz/.zip |
| 分片 + 整体 SHA-256 | 业界标配，防篡改/防损坏 |
| 分片级并发 + worker 池 | 同一文件多分片并发上传，充分利用带宽 |
| 流式打包下载 | 边压缩边发送，省磁盘、低延迟 |
| 无鉴权 + 网关代理 | 上游 Gateway 注入用户身份头，服务只做文件逻辑 |

## 遇到的问题
| 问题 | 解决方案 |
|------|---------|
| Bash 安全分类器暂时不可用 | 改用 Read/Write 文件工具，不受影响 |

## 资源
- tus.io 协议规范（后续实现 tus Handler 时参考）：https://tus.io/protocols/resumable-upload
- chi 路由：github.com/go-chi/chi
- （待定）tusd 官方库参考：github.com/tus/tusd（仅作协议实现参考，不一定依赖）

## 视觉/浏览器发现
<!-- 关键：每执行2次查看/浏览器操作后必须更新此部分 -->
<!-- 多模态内容必须立即以文本形式记录 -->
- 暂无（本会话未使用浏览器/查看图片）

---
*每执行2次查看/浏览器/搜索操作后更新此文件*
*防止视觉信息丢失*
