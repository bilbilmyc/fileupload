# ADR-0006: BatchService.meta 接口分离（FileStore + BlobStore）

## 状态
已决定（2026-06-24）

## 背景
`BatchService` 在批量复制（BatchCopy / copyDirChildren）路径上，需要在创建新文件记录后增加 blob 引用计数。但 `internal/domain/batch.go:223-228` 的 `IncrBlobRefIfAvailable` 用了类型断言（duck typing）来弥合 `FileStore` 不含 `IncrBlobRef` 的缺口：

```go
func (s *BatchService) IncrBlobRefIfAvailable(ctx context.Context, sha256 string) error {
    if bs, ok := s.meta.(interface{ IncrBlobRef(context.Context, string) error }); ok {
        return bs.IncrBlobRef(ctx, sha)
    }
    return nil  // 静默 no-op
}
```

两个调用点（`batch.go:210`、`batch.go:272`）都用 `_ = s.IncrBlobRefIfAvailable(...)` 吞错。结果：
- `IncrBlobRef` 失败时操作员看不到任何信号（日志、错误码、监控均无）
- 当某 `meta` 实现恰好不实现 `BlobStore` 时，ref_count 永远不会被增加 — Content-Addressed Storage 不变量被静默破坏

## 决策
将 `BatchService.meta` 字段类型升格为 `interface { FileStore; BlobStore }` 的匿名复合，删除 `IncrBlobRefIfAvailable` 方法，调用点改为：

```go
if err := s.meta.IncrBlobRef(ctx, file.SHA256); err != nil {
    log.Printf("[batch] 复制后引用计数失败 sha=%s: %v", file.SHA256, err)
    // 不回滚 PutFile — 属于 refCounter 端口的范畴，留待未来加固
}
```

错误处理遵循 BatchService 现有的"逐项失败容忍"契约（与 PutFile/GetFile 错误处理对齐），不升级为批量级 fatal。

## 备选方案
- **`meta` 类型升格为 `domain.Metadata`（全集）**：暴露 30+ 方法给 BatchService，违反接口分离原则。驳回。
- **失败时回滚 PutFile**（DeleteFile 撤销孤儿记录）：引入新的失败模式（DeleteFile 也可能失败），必须配套一致性扫描。属于 refCounter 端口设计，留待后续 ADR。
- **保留 `IncrBlobRefIfAvailable` 但去掉类型断言**：需要构造一个保证包含 BlobStore 的子类型，等同于本 ADR 决策但更绕。驳回。

## 影响
- `NewBatchService` 签名变化：第 4 个参数从 `FileStore` 变为匿名复合接口。`cmd/server/main.go:165` 传入的 `metaFacade` 已满足新约束，零 wire 改动。
- 单元测试 `mockMetadata`（`mock_test.go:215`）已实现 `IncrBlobRef`，自动满足新约束。
- 新增回归测试 `TestBatchCopy_IncrementsRefCount` 与 `TestBatchCopyDir_IncrementsRefCount`，分别覆盖两个调用点。
- 若 `IncrBlobRef` 失败，操作员通过 `log.Printf` 可见；reaper.go:233-254 仍只检测不修复，根治属于 refCounter 端口（候选 #3）。
- `Rule of Three`：若将来出现第二个 service 需要 `FileStore + BlobStore` 组合，再升格为命名接口（如 `domain.FileBlobStore`）。

## 相关
- ADR-0001（物理文件存储策略）— 确立了 ContentBlob 概念
- 候选 #3（refCounter 端口）— 根治引用计数纪律，本 ADR 是其前置清理
