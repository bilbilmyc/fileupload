# ADR-0001: 物理文件存储策略

## 状态
已实施（2026-06-22）

## 背景
最初物理文件按 `namespace/filename` 扁平存储（内容寻址），目录结构仅由 SQLite 元数据维护。用户反馈运维时需要直接从文件系统拷贝目录，扁平结构无法满足需求。

## 决策
Finalize 阶段仍按扁平路径存储（兼容内容寻址），SubmitDir 阶段将物理文件搬到层级路径：

- **Finalize 写入：** `namespace/filename`（扁平）
- **SubmitDir 重组：** 复制到 `namespace/subdir/filename` → 删除扁平文件 → 更新 ContentBlob.StoragePath
- 使用 Storage 端口的 Open + Write + Delete 实现（不新增 Move 方法）

## 备选方案
- **全层级存储**：Finalize 直接写入层级路径。被否决，因为 Finalize 时不知道文件在目录树中的位置，且单文件上传不需要层级。
- **软链接**：层级路径用符号链接指向扁平文件。被否决，因为跨文件系统拷贝时不保留链接。
- **导出命令**：提供 CLI 命令按需还原目录结构。被否决，因为用户要求上传后立即可用。

## 影响
- 物理文件会有重复写入（先扁平后层级），大文件有一定性能开销
- ContentBlob.StoragePath 需要在 SubmitDir 中更新
- 单文件上传不受影响（不走 SubmitDir）
- 目录上传后的文件可在 `storage/data/default/<dir>/` 中直接访问
