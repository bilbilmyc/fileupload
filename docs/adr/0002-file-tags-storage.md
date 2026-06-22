# ADR-0002: 文件标签存储方案

## 状态
已实施（2026-06-22）

## 背景
批量管理功能需要为文件添加标签元数据。需要在 SQLite 中选择存储方式。

## 决策
使用关系型关联表 `file_tags`：

```sql
CREATE TABLE file_tags (
    file_id    TEXT NOT NULL REFERENCES files(file_id) ON DELETE CASCADE,
    tag        TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (file_id, tag)
);
```

FileMetadata 领域模型添加 `Tags []string` 字段用于 JSON 序列化。

## 备选方案
- **JSON 字段**：在 `files` 表加 `tags TEXT` 列存 JSON 数组。被否决，因为不利于标签独立查询和跨文件统计，且 Schema 变更成本高。
- **标签表 + 关联表**：独立的 `tags` 主表 + `file_tags` 关联表。被否决，目前标签只是简单字符串，不值一个独立表的开销。后续如需标签管理（重命名、合并）可升格为主表。

## 影响
- 查询文件标签需 JOIN：`SELECT tag FROM file_tags WHERE file_id = ?`
- 设置标签是事务操作（DELETE ALL + INSERT ALL），保证原子性
- 删除文件时通过 `ON DELETE CASCADE` 自动清理标签
