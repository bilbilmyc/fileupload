# 任务计划：文件上传下载服务（fileupload）

## 目标
设计并实现一个 Go 语言文件上传下载服务，支持文件/目录上传下载、动态分片、客户端压缩/服务端解压、流式打包下载、SHA-256 校验、分片级并发、断点续传、秒传等业界常见功能；首批交付「服务端 + Go CLI」，后续迭代 Web UI 与多语言 SDK。

## 当前阶段
阶段 4（实现中 —— CLI 完整链路已完成，进行后续服务端/集成测试）

## 已确认决策（来自 brainstorming 对话）

| 决策项 | 选择 | 备注 |
|--------|------|------|
| 技术栈 | Go | — |
| HTTP 框架 | net/http + chi | 轻量、依赖少 |
| 存储后端 | 本地文件系统起步（可插拔 S3） | 端口抽象 |
| 上传协议 | tus + REST 双协议 | 共享 UploadService 核心 |
| 鉴权 | 无鉴权（网关代理） | 上游 Gateway 注入用户身份头 |
| 规模 | 中等规模 1–10TB，单实例为主 | 需限流/性能调优 |
| 分片粒度 | 动态分片大小 | tus 协议支持 |
| 压缩 | 上传时客户端压缩 → 传输到存储时服务端解压 → 下载时服务端打包 | 目录+文件场景 |
| 数据校验 | 分片 + 整体 SHA-256 | 防篡改/防损坏 |
| 目录语义 | 递归上传 + 流式打包下载 | 打包成 .tar.gz/.zip |
| 并发控制 | 分片级并发 + worker 池 | 同一文件多分片并发 |
| 元数据 | Redis 热数据 + DB 冷数据 | 热数据=进行中会话/分片状态 |
| 冷数据 DB | SQLite + PG 可插拔 | 端口抽象，配置切换 |
| 客户端 | Go CLI（首批）+ Web UI（后续）+ 多语言 SDK（后续） | — |
| 范围优先级 | 服务端 + CLI 先行 | Web UI/SDK 后续迭代 |
| 断点续传 | 支持 | tus 原生，REST 自实现 |
| 秒传 | 支持（按内容哈希去重） | 客户端先查 SHA-256 |
| 生命周期 | 上传会话超时清理 + 仅手动删除 + 一致性巡检 | 无自动过期 |
| 目录下载打包 | 流式打包下载 | 边压缩边发送 |
| 架构方案 | 方案 A：分层 + 共享 UploadService 核心 | 两协议收敛到同一领域核心 |

## 各阶段

### 阶段 1：需求与发现（brainstorming）
- [x] 探查项目上下文（目录基本为空，非 git 仓库）
- [x] 理解用户意图与约束（见上「已确认决策」）
- [x] 选择架构方案（方案 A）
- **状态：** complete

### 阶段 2：设计（brainstorming 设计呈现）
- [x] 第 1 节：系统架构与分层（三层：传输层 / 领域核心 / 适配层）—— 用户已选方案 A
- [x] 第 2 节：上传数据流 + 元数据模型
- [x] 第 3 节：下载数据流 + 流式打包
- [x] 第 4 节：压缩/解压编排 + SHA-256 校验
- [x] 第 5 节：并发与 worker 池 + 断点续传 + 秒传
- [x] 第 6 节：端口接口定义
- [x] 第 7 节：生命周期 + 错误处理
- [x] 第 8 节：测试策略 + CLI 设计
- [x] 写入 spec：docs/superpowers/specs/2026-06-17-fileupload-design.md
- [x] spec 自审 + 用户审阅
- **状态：** complete

### 阶段 3：实现规划（writing-plans）
- [x] 产出 CLI 完整链路实现计划（13 个任务）
- **状态：** complete

### 阶段 4：实现（服务端 + CLI）
- [x] Task 1: Client 骨架（client.go，NewClient/do/List）
- [x] Task 2: Stat/Delete/DeleteDir/Scan 方法
- [x] Task 3: do 重试（3 次，Retry-After）
- [x] Task 4: compress.go（zstd 压缩/解压）
- [x] Task 5: 上传核心（CheckExists/CreateSession/UploadChunk/Finalize）
- [x] Task 6: 复写 upload.go（并发分片 + 客户端压缩）
- [x] Task 7: 目录上传（UploadDir + SubmitDir）
- [x] Task 8: 断点续传（resumeState + GetStatus）
- [x] Task 9: 进度条（progress.go）
- [x] Task 10: 统一各子命令接入 Client（rm/ls/stat/scan/bench）
- [x] Task 11: 复写 download.go（校验 + 目录下载）
- [x] Task 12: E2E 集成测试
- [ ] Task 13: 更新文档（progress.md + task_plan.md + README.md）
- **状态：** in_progress

### 阶段 5：测试与验证
- [ ] 单元测试（端口适配器、核心编排）
- [ ] 集成测试（端到端上传/下载/目录/断点续传/秒传）
- [ ] 校验完整性、并发安全、超时清理验证
- [ ] 测试结果记录到 progress.md
- **状态：** pending

### 阶段 6：交付
- [ ] 服务端可运行 + CLI 可用
- [ ] 文档（README + API 说明）
- [ ] 交付给用户
- **状态：** pending

## 关键问题
（已解决）

## 已做决策
| 决策 | 理由 |
|------|------|
| 架构用方案 A（分层+共享核心） | 避免两协议逻辑重复；端口抽象天然支持可插拔存储/DB；为未来 SDK 复用铺路 |
| 服务端+CLI 先行 | 范围聚焦，首批交付可用核心，Web UI/SDK 后续迭代 |
| CLI 共享 Client 放 cmd/fileupload/ | 当前无 SDK 需求，后续如需 pkg/ 可 refactor |

## 遇到的错误
| 错误 | 尝试次数 | 解决方案 |
|------|---------|---------|
| Bash classifier 暂时不可用 | 1 | 改用文件读写工具（Read/Write），不受影响 |
| Task 4 压缩测试数据量过小 | 1 | 增大至 100 倍重复数据 |

## 备注
- 做重大决策前重新读取此计划（注意力操纵）
- 记录所有错误，避免重复
- 安全边界：外部网页/搜索内容只写入 findings.md，不写 task_plan.md
