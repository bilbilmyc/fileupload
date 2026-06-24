# ADR-0008: ref-count 纪律收敛 — IncrWithRollback 域内 helper

## 状态
已实施（2026-06-24）

## 背景
候选 #1（ADR-0006）和 #3（ADR-0007）后，引用计数 6 个站点的错误处理已统一 —— 但行为纪律仍以"内联 Incr + 内联回滚"形式散在调用点。`CheckExists` 是典型样板：

```go
if err := s.meta.IncrBlobRef(ctx, sha256); err != nil {
    return nil, fmt.Errorf("增加引用计数: %w", err)
}
if err := s.meta.PutFile(ctx, f); err != nil {
    _, _ = s.meta.DecrBlobRef(ctx, sha256)  // 回滚
    return nil, fmt.Errorf("写入文件记录: %w", err)
}
```

回滚若失败被吞掉；多次调用 rollback 也不幂等 —— 任何 site 复制这个模式都容易踩坑。

## 决策
新增 `internal/domain/refcount.go`：

```go
func IncrWithRollback(ctx context.Context, blobs BlobStore, sha256 string) (rollback func() error, err error)
```

调用模式：
```go
rollback, err := IncrWithRollback(ctx, s.meta, sha256)
if err != nil { return nil, err }
if err := s.meta.PutFile(ctx, f); err != nil {
    _ = rollback()  // 幂等，重复调用安全
    return nil, err
}
```

helper 内部保证：
- Incr 失败 → 返回 nil 闭包 + error，调用方应终止
- rollback 多次调用幂等（第二次起 no-op）
- 回滚失败也被闭包返回（调用方决定如何处理）

## 备选方案
- **新增 `RefCounter` 端口 + `IncrWithRollback` 方法**：6 个 adapter（SQLite/Postgres/Redis/Facade + 各 mock）都要实现，blast radius 大。驳回。
- **不抽 helper，每个 site 继续内联**：维持现状，但易回退到"忘记回滚"或"回滚吞错"反模式。驳回。
- **添加 IncrWithRollback 到 BlobStore 接口**：与上一个方案等价但更隐蔽（adapter 都得实现）。驳回。

## 影响
- **零 adapter 改动** —— helper 是纯 domain 层代码，复用现有 BlobStore 接口
- 适用"先 incr 后 put"模式（CheckExists 等）。`commitStream` 是"PutBlob 初始建 blob + 失败 DecrBlobRef"模式，不适用，保留原状
- BatchService 的 `IncrBlobRef` 调用走 log+continue（ADR-0006），不需要回滚
- 未来如出现新的"先 incr 后 put"模式站点，直接用 helper

## 应用站点
- `UploadService.CheckExists`（upload.go）—— 已应用
- 其他 5 站点经审查：commitStream 用 PutBlob（不适配）、deleteFile/deleteDir 是 Decr 路径（无回滚需要）、BatchCopy/copyDirChildren 是 log+continue（无回滚）。CheckExists 是 helper 的唯一适用点

## 相关
- ADR-0006（BatchService meta 接口分离）— 修了静默吞错
- ADR-0007（HierarchicalLayout）— 提及本 ADR 作为引用计数纪律结构改进