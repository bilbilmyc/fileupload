# ADR-0007: HierarchicalLayout 集中路径布局约定

## 状态
已实施（2026-06-24）

## 背景
ADR-0001 承诺 SubmitDir 后物理文件位于 `namespace/subdir/filename`，Finalize 阶段位于 `namespace/filename`。两条约定散在 5 个调用点（`fmt.Sprintf` 内联）：

| 文件 | 行 | 格式 |
|---|---|---|
| `upload.go` | 350 | `fmt.Sprintf("%s/%s", ns, fileName)` — Finalize 扁平路径 |
| `upload.go` | 470 | `fmt.Sprintf("%s/%s/%s", ns, dirName, entry)` — SubmitDir 层级路径 |
| `upload.go` | 472-479 | `storage.Open/Write/Delete` 三步搬移（静默吞错） |
| `download.go` | 248 | `fmt.Sprintf("%s|%d|%s\n", ...)` — 下载清单行 |
| `finalize.go` | (无) | 间接通过 `storagePath` 参数 |

代码读者无法一眼看清"扁平 vs 层级"区别，且搬移失败被静默吞掉，运维只能从文件系统不一致现象发现。

## 决策
新增 `internal/domain/layout.go`：

```go
type HierarchicalLayout struct{}

func NewHierarchicalLayout() *HierarchicalLayout
func (l *HierarchicalLayout) FlatPath(namespace, fileName string) string
func (l *HierarchicalLayout) HierarchicalPath(namespace, dirName, entryPath string) string
func (l *HierarchicalLayout) Move(ctx, storage Storage, src, dst string) error
```

调用点统一收敛：

| 原 | 现 |
|---|---|
| `upload.go:350 fmt.Sprintf` | `s.layout.FlatPath(ns, fileName)` |
| `upload.go:470 fmt.Sprintf` | `s.layout.HierarchicalPath(ns, dir, entry)` |
| `upload.go:472-479 Open/Write/Delete 静默` | `s.layout.Move(ctx, storage, src, dst)`，失败 log+continue |

UploadService 构造时自动持有 `*HierarchicalLayout`（无状态，可全局共享）。

## 备选方案
- **package-level 函数**（`FlatPath(...)` 直接导出）：无 namespace，但调用点失去"路径布局"语义。驳回。
- **加状态字段（如 root prefix）**：当前无此需求，过早抽象。驳回，留待未来。
- **保留内联 fmt.Sprintf**：当前 bug 的根本原因。驳回。

## 影响
- `upload.go` SubmitDir 段从 ~40 行（含错误吞掉的 8 行）缩到 ~20 行，可读性显著提升
- 搬移失败不再静默 — operator 可见 log，便于 reaper 后续处理
- HierarchicalLayout 是无状态类型，可作为 UploadService 的隐含依赖，无需传参
- ADR-0001 的"扁平 → 层级"语义在代码中第一次有 single source of truth

## 引用计数纪律（附带）
经过候选 #1（ADR-0006）和本 ADR 的修改，6 个引用计数站点的错误处理已统一：

| 站点 | 处理方式 |
|---|---|
| CheckExists (`upload.go:74-95`) | IncrBlobRef → PutFile；PutFile fail → DecrBlobRef 回滚 ✓ |
| commitStream (`finalize.go:127-153`) | PutBlob(RefCount=1) → PutFile；PutFile fail → DecrBlobRef 回滚 ✓ |
| deleteFile (`upload.go:583-605`) | DecrBlobRef；newCount<=0 → storage.Delete ✓ |
| deleteDir (`upload.go:607-625`) | 递归 deleteFile，继承回滚 ✓ |
| BatchCopy (`batch.go:206`) | PutFile → log+continue on IncrBlobRef fail（ADR-0006）✓ |
| copyDirChildren (`batch.go:268`) | 同上 ✓ |

行为一致性已达成。如未来需要进一步抽象（`RefCounter` 端口 + `IncrWithRollback(sha) (rollback, err)`），属独立 refactor。

## 相关
- ADR-0001（物理文件存储策略）— 本 ADR 是其代码层落地
- ADR-0006（BatchService.meta 接口分离）— 配套修复引用计数静默吞错