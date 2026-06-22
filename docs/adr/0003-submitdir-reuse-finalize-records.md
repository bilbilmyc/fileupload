# ADR-0003: SubmitDir 复用 Finalize 记录

## 状态
已实施（2026-06-22）

## 背景
目录上传时，每个文件先经过 Finalize（创建 FileMetadata 记录），然后 SubmitDir 再为每个文件创建新记录。导致同一个文件在根目录（Finalize 记录）和目录树下（SubmitDir 记录）各出现一次。

## 决策
SubmitDir 不再创建新的 FileMetadata 记录，而是复用 Finalize 创建的记录，调用 `ReparentFile` 更新其 `parent_id` 和 `path`。

新增接口方法：
```
ReparentFile(ctx, fileID, parentID, path) error
```

SQLite 实现：`UPDATE files SET parent_id = ?, path = ? WHERE file_id = ?`

## 备选方案
- **创建新记录 + 删除旧记录**：先创建目录树记录，再删除 Finalize 的扁平记录。问题是 blob 引用计数管理复杂。
- **Finalize 时预知目录信息**：前端在 init 时传入目录路径。被否决，因为 Finalize 与 SubmitDir 是解耦的，Finalize 不应感知目录上下文。

## 影响
- SubmitDir 后根目录不再出现重复文件
- 现有 `FileMetadata.FileID` 保持不变（不会因入目录而变）
- `Path` 字段从文件名更新为目录相对路径（如 `subdir/file.txt`）
- Metadata 端口需要新增 `ReparentFile` 方法，所有实现需同步更新
- 历史数据中已有重复记录，需手动清理（可通过批量删除处理根目录下的孤立文件）
