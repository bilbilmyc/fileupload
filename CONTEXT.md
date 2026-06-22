# fileupload 领域词汇表

## 核心概念

### 文件上传 (File Upload)
单个文件通过 tus 或 REST 协议上传至服务端。上传流程：初始化会话（InitSession）→ 上传分片（UploadChunk）→ 完成（Finalize）。完成时计算 SHA-256 校验和，进行内容寻址去重。

### 目录上传 (Directory Upload)
浏览器端通过 `webkitdirectory` 获取整个目录的文件列表，每个文件独立走上传流程。所有文件上传完成后，调用 SubmitDir 提交 manifest，服务端据此构建目录树并将物理文件重新组织为层级结构。

### 目录树 (Directory Tree)
文件和子目录通过 `parent_id` 自引用字段组织成树形结构。根节点 `parent_id` 为空。SQLite `files` 表维护所有节点关系。目录上传时自动根据 `entry.Path` 中的 `/` 分隔符创建中间目录节点。

### 内容寻址存储 (Content-Addressed Storage)
物理文件通过 SHA-256 内容哈希寻址。`ContentBlob` 记录文件的存储路径和引用计数（`ref_count`）。多个逻辑文件可以指向同一物理 blob，实现去重。

### 层级存储 (Hierarchical Storage)
目录上传完成后，SubmitDir 会将物理文件从扁平的 `namespace/filename` 搬移到 `namespace/subdir/filename`，并更新 ContentBlob 记录。目的是让运维人员可以直接从文件系统拷贝目录结构。

### 上传会话 (Upload Session)
单个文件上传的生命周期。状态机：`active` → `finalizing` → `completed`。包含 session_id、SHA-256、压缩格式、文件名等信息。存储在 Redis 热数据层。

### 压缩格式 (Compression Format)
支持的压缩/归档格式：`none`、`zstd`、`gzip`、`tar.gz`、`tar.zst`、`zip`。客户端上传时可选压缩（zstd），服务端解压后存储原始内容。下载时可选打包格式（tar.gz/zip）。

### 秒传 (Instant Upload / Dedup)
通过 `HEAD /v1/files?sha256=xxx` 预检文件是否已存在。命中则直接增加 blob 引用计数并创建逻辑文件记录，无需重新上传。

## 批量管理

### 批量操作 (Batch Operation)
对多个文件/目录同时执行的操作。支持：删除（delete）、下载（download 打包为 zip/tar.gz）、移动（move）、复制（copy）、标记（tag）。通过 `POST /v1/batch/*` 端点执行。

### 文件标签 (File Tag)
附加在文件上的标签元数据。通过 `file_tags` 关联表（file_id, tag）实现 N:M 关系。标签用于分类和筛选文件。

### 操作历史 (Operation History)
客户端侧记录的批量操作历史，包含操作类型、文件数量、时间、状态等信息。用于审计和回溯。

## 目录管理

### 目录节点 (Directory Node)
`files` 表中 `is_dir=true` 的记录。通过 `parent_id` 形成树状层次。

### 目录 Manifest
前端提交的目录清单，包含目录名和文件条目（path + file_id 对）。SubmitDir 据此构建目录树。

### 文件重组 (File Restructuring)
SubmitDir 的最后阶段：将物理文件从扁平路径搬移到层级路径，同时更新 ContentBlob 的存储路径记录。
