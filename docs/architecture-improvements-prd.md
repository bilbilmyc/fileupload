# Architecture Improvements — PRD

**Status:** Completed

✅ **Item 1 completed** — `domain.Metadata` split into `SessionStore`/`BlobStore`/`FileStore`/`AdminStore`, commit `65ad793`
✅ **Item 2 completed** — shared mock package `internal/testutil`, transport mocks consolidated`
✅ **Item 3 completed** — `BatchService` depends on `FileDeleter`/`FileMover`/`DownloadPacker` interfaces`
✅ **Item 4 completed** — `Finalize` decomposed into `mergeChunks`/`verifyStream`/`commitStream` phases, 13 new tests`
✅ **Item 5 (Phase 1) completed** — `HotStore` interface extracted, `Facade.hot` changed from `*RedisStore` to `HotStore``
✅ **Item 6 completed** — batch handlers consolidated into table-driven `BatchHandle``
**Date:** 2026-06-23
**Source:** `fileupload` architecture review (2026-06-23)

---

## Problem Statement

The `fileupload` codebase follows a clean hexagonal architecture (domain core + adapter ports), but several structural issues create maintenance friction and limit testability:

1. The **`domain.Metadata`** port interface has grown to **25 methods**, mixing 7 unrelated concerns (sessions, blobs, files, tags, directory tree, audit logs, admin counts). This violates the Interface Segregation Principle — any port change forces recompilation of all consumers, and mock files (739 combined lines across two packages) must mirror the entire interface.

2. The **`metadata.Facade`** is a pass-through proxy, not a true Facade — it delegates to hot/cold stores without adding cross-store coordination, caching, or fallback logic. The hot store is locked to a concrete `*RedisStore` type, preventing alternative implementations.

3. **Mock implementations are duplicated** across `domain/mock_test.go` (527 lines) and `transport/mock_test.go` (212 lines), creating two maintenance targets for the same port interfaces.

4. **`BatchService` depends on concrete domain services** (`*UploadService`, `*DownloadService`) rather than interfaces, preventing isolated batch-layer testing.

5. **The `Finalize` method** is a 170-line procedural monolith handling merge, decompress, hash-verify, write, refcount, and cleanup in a single scope with no rollback — partial failures can orphan files.

6. **Five `BatchHandler` handlers** follow an identical decode-validate-call-respond pattern, duplicated across endpoints.

---

## Solution

A phased programme of architectural refactoring targeting deepening — turning shallow pass-through modules into deep ones with real leverage:

1. **Split `domain.Metadata`** into 4 seam-aligned sub-interfaces: `SessionStore`, `BlobStore`, `FileStore`, `AdminStore`. Keep a composed `Metadata` interface for convenience. Each domain service declares only the sub-interfaces it needs.

2. **Extract shared test mocks** from `domain/mock_test.go` and `transport/mock_test.go` into a single `internal/testutil` package.

3. **Define interface dependencies for `BatchService`** — replace concrete `*UploadService`/`*DownloadService` pointers with operation-specific interfaces (`FileDeleter`, `FileMover`, `DownloadPacker`).

4. **Pipeline the `Finalize` method** into independently-testable phases: `Merger` (chunks → temp file), `Verifier` (decompress + hash), `Committer` (write + refcount + record). Add cleanup-on-error contracts.

5. **Upgrade `Facade` to a real Facade** — make `hot` an interface (`HotStore`), add read-through fallback (hot miss → cold query → repopulate hot).

6. **Consolidate batch handlers** into a table-driven dispatch: one generic handler routing by action name.

---

## User Stories

1. As a **domain service implementer**, I want `UploadService` to declare only the methods it needs (`SessionStore` + `BlobStore`), so that I can see its dependencies at a glance without reading the full 25-method interface.

2. As a **domain service implementer**, I want `BatchService` to depend on interfaces rather than concrete `*UploadService`/`*DownloadService`, so that I can test batch logic without instantiating the full service graph.

3. As a **test author**, I want a single shared mock package (`internal/testutil`) instead of two copies, so that adding a method to a port interface breaks exactly one mock file.

4. As a **test author**, I want to mock only 5 methods when testing admin endpoints instead of all 25, so that my tests are smaller and more focused.

5. As an **operations engineer**, I want sessions to survive a Redis restart via cold-store fallback in `Facade`, so that active uploads are not lost during Redis maintenance.

6. As a **developer debugging upload failures**, I want the `Finalize` pipeline to have explicit cleanup-on-error contracts, so that partial progress doesn't orphan data files on disk.

7. As a **developer extending batch operations**, I want to add a new batch action by adding two lines to a dispatch table, so that I don't copy-paste the decode-validate-respond pattern.

8. As a **release engineer**, I want smaller compile-time dependency graphs so that changing one interface method doesn't trigger recompilation of unrelated packages.

---

## Implementation Decisions

### Decision 1: Interface Split Boundaries

`domain.Metadata` (25 methods) splits into 4 interfaces, aligned to the natural hot/cold storage split:

| Interface | Methods | Responsibility |
|-----------|---------|----------------|
| `SessionStore` | 7 | CreateSession, GetSession, UpdateOffset, ListChunks, DeleteSession, TouchSession, ListExpiredSessions |
| `BlobStore` | 5 | GetBlobBySha, PutBlob, UpdateBlobStorage, IncrBlobRef, DecrBlobRef |
| `FileStore` | 8 | PutFile, GetFile, GetFileByPath, ListChildren, ListRoot, DeleteFile, ListFilesByBlob, ReparentFile, UpdateFileParent, SetFileTags, GetFileTags, DeleteFileTags |
| `AdminStore` | 5 | WriteAuditLog, ListAuditLogs, AdminCountFiles, AdminCountBlobs, AdminTotalBlobSize |

A composed `Metadata` interface remains for existing callers that genuinely need all operations:
```go
type Metadata interface {
    SessionStore
    BlobStore
    FileStore
    AdminStore
}
```

### Decision 2: BatchService Interface Dependencies

`BatchService` currently takes concrete `*UploadService` and `*DownloadService`. These are replaced with operation-specific interfaces defined in `domain/batch.go`:

```go
type FileDeleter interface {
    DeleteFile(ctx, fileID, namespace) error
    DeleteDir(ctx, dirID, recursive, namespace) error
}

type FileMover interface {
    MoveFile(ctx, fileID, targetDirID, namespace) error
}

type DownloadPacker interface {
    StreamBatch(ctx, fileIDs, format, w) error
}
```

The concrete services implement these interfaces; `BatchService` accepts them at construction.

### Decision 3: Mock Consolidation

New package `internal/testutil` (test-only, excluded from production builds via `_test.go` naming or build tags) with exported mock types. Existing `domain/mock_test.go` becomes a thin re-export layer or is deleted; `transport/mock_test.go` imports from `testutil`.

### Decision 4: Finalize Pipeline

The `Finalize` method is decomposed into three phases with explicit input/output types and cleanup registrations:

```
Finalize(sessionID)
  → Merger.merge(ctx, chunks) → (tempFile, cleanup, error)
  → Verifier.verify(ctx, tempFile, expectedSHA256) → (verifiedReader, error)
  → Committer.commit(ctx, reader) → (blob, file, error)
  → cleanupStaleTempFiles()
```

Each phase is independently testable with known inputs. The `Merger` phase registers a cleanup closure; if any subsequent phase fails, the cleanup chain executes.

### Decision 5: HotStore Interface

`Facade.hot` changes from concrete `*RedisStore` to an interface:

```go
type HotStore interface {
    CreateSession(ctx, session) error
    GetSession(ctx, id) (*UploadSession, error)
    UpdateOffset(ctx, id, sliceIndex, sliceSha, addBytes) error
    ListChunks(ctx, id) ([]ChunkInfo, error)
    DeleteSession(ctx, id) error
    TouchSession(ctx, id, ttl) error
    ListExpiredSessions(ctx) ([]string, error)
    Close() error
}
```

`RedisStore` already implements these methods — no code change needed in the adapter, only the type of `Facade.hot`.

Phase 2 adds read-through fallback in `Facade.GetSession`: if `hot.GetSession` returns nil, query `cold`, repopulate `hot`, return.

### Decision 6: BatchHandler Consolidation

Replace 5 separate handler methods with one handler that reads `action` from the URL path and dispatches via a table:

```
POST /v1/batch/{action}
  action: delete | download | move | copy | tags
```

---

## Testing Decisions

### What makes a good test

Each test should exercise behavior through the narrowest public interface possible. After the refactoring:

- **`BlobStore` tests** exercise blob CRUD + refcount through `*SQLiteStore` (no Facade, no Redis)
- **`SessionStore` tests** exercise session lifecycle through `*RedisStore` directly (no Facade, no SQLite)
- **`BatchService` tests** exercise batch orchestration through `FileDeleter` / `FileMover` / `DownloadPacker` mocks (no real upload/download services)
- **`Finalize` phase tests** exercise each phase (`Merger`, `Verifier`, `Committer`) with known fixture data and verify output + cleanup

### Test modules

| Module | Type | Prior art |
|--------|------|-----------|
| `internal/testutil` | Shared mock types | Refactored from `domain/mock_test.go` |
| `domain/upload_test.go` | Unit (mocked adapters) | `TestUploadService_*` |
| `domain/batch_test.go` | Unit (mocked FileDeleter/FileMover) | New — follows `upload_test.go` pattern |
| `domain/finalize_test.go` | Unit (phase-by-phase) | New — pipeline decomposition |
| `adapters/metadata/facade_test.go` | Unit (mocked HotStore + ColdStore) | Existing facade tests updated |
| `transport/batch_handler_test.go` | Unit (table-driven batch) | Existing batch handler tests updated |

### Seams

Testing happens at the **interface seam**, not the concrete implementation. After refactoring:

- Batch tests mock `FileDeleter` (3 methods) instead of instantiating `*UploadService` (10 methods) with mock adapters underneath
- Finalize tests call `Merger.Merge` with known chunk descriptors, not through the HTTP handler
- Facade tests mock `HotStore` interface instead of requiring `miniredis`

---

## Out of Scope

- **S3 storage backend** — the `Storage` port already supports S3; no changes to `adapters/storage/s3.go`
- **SQL ↔ PostgreSQL query differences** — `ColdStore` implementations are equivalent; no query changes
- **CLI or SDK changes** — all changes are internal to the server package
- **Configuration schema changes** — `DatabaseConfig.PG` was already added; the current PRD does not introduce new config fields
- **Observability** — metrics, tracing, structured logging are out of scope for this phase
- **Authentication / authorization changes** — JWT middleware and auth handler remain as-is

---

## Further Notes

- The refactoring is incremental — each improvement is independently deployable and testable
- Priority order matters: Interface split (#1) should precede Mock consolidation (#2) and BatchService deps (#3), because those depend on the new interface boundaries
- The Facade upgrade (#5) has two phases; Phase 1 (interface extraction) is low-risk and can be done early; Phase 2 (read-through fallback) requires Redis + SQLite coordination design
- ADR-0001 through ADR-0005 are not contradicted by any of the proposed changes
