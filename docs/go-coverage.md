# Go 测试覆盖率报告（v0.10.0）

## 跑法

```bash
go test -coverprofile=cover.out ./...
go tool cover -func=cover.out | tail -30
go tool cover -html=cover.out -o cover.html
```

HTML 报告生成到 `cover.html`，可在浏览器打开。

## 当前覆盖率（v0.10.0 snapshot）

```
Package                                            Coverage
------------------------------------------------    --------
internal/adapters/auth                             87.7%
internal/adapters/compressor                       81.1%
internal/adapters/hasher                            93.3%
internal/adapters/metadata                         37.3% (Postgres 集成测试本地化前略低)
internal/adapters/storage                          59.0%
internal/config                                     60.4%
internal/domain                                    67.8%
internal/lifecycle                                  93.5%
internal/metrics                                   N/A (只有 const 初始化)
internal/transport                                  64.7%
sdk/go/fileupload                                  25.5% (upload 流依赖 mock server)
deploy/grafana / deploy/prometheus                   N/A (YAML 验证测试)
cmd/server                                          0% (main 函数，无法覆盖)
sdk/js (TS)                                       N/A (见 docs/coverage.md)
web (TS)                                          N/A (见 docs/coverage.md)

TOTAL                                              48.2%
```

## 重点模块解读

| Package | Coverage | 说明 |
|---|---|---|
| **`hasher`** | 93.3% | SHA-256 hash 函数，单元测试覆盖完整 |
| **`lifecycle`** | 93.5% | Reaper + Scanner（v0.2.0 加测试后提升） |
| **`adapters/auth`** | 87.7% | JWT 签发/验证覆盖 |
| **`compressor`** | 81.1% | zstd/gzip/zip/tar 编码 |
| **`domain`** | 67.8% | 核心业务逻辑（含 deleteDir 原子性 T1-T5） |
| **`transport`** | 64.7% | HTTP handlers（含 v0.10.0 新增 Admin 测试） |
| **`adapters/storage`** | 59.0% | LocalFS + S3Storage |
| **`adapters/metadata`** | 37.3% | SQLite/Postgres（Postgres 部分需集成测试） |
| **`sdk/go`** | 25.5% | 高层 API（Upload 等需 httptest.Server） |

## SDK 详细覆盖

```
File                                              Function              Coverage
sdk/go/fileupload/extended.go:24                  GetUploadStatus      69.2%
sdk/go/fileupload/extended.go:45                  CancelUpload         70.0%
sdk/go/fileupload/extended.go:63                  PreviewURL          100.0%
sdk/go/fileupload/extended.go:69                  Preview               0.0% (返回 *http.Response 未断言 body)
sdk/go/fileupload/upload.go:286                   SubmitDir            73.3%
sdk/go/fileupload/download.go:138                 DeleteDir             0.0%
sdk/go/fileupload/download.go:152                 BatchDelete           0.0%
sdk/go/fileupload/download.go:179                 BatchCopy             0.0%
sdk/go/fileupload/download.go:205                 Scan                  0.0%
sdk/go/fileupload/upload.go:28                    Upload                0.0%
sdk/go/fileupload/upload.go:71                    UploadReader          0.0%
sdk/go/fileupload/upload.go:321                   CreateSession         0.0%
sdk/go/fileupload/upload.go:348                   UploadChunk           0.0%
sdk/go/fileupload/upload.go:371                   GetStatus             0.0%
sdk/go/fileupload/upload.go:417                   Finalize              0.0%
```

## 改进方向（未来 sprint）

1. **`sdk/go` 补 Upload 流测试**（httptest.Server mock 后端）
2. **`adapters/metadata` Postgres 部分**通过集成测试覆盖
3. **`internal/transport` 核心 handler**（upload/download）端到端覆盖
4. **目标**：核心 SDK 包 ≥ 80%，核心 domain ≥ 90%

## 排除规则

当前未排除任何生产代码。如需排除 vendor / main：

```go
// 在 Go 中通常用 build tag 而非排除：
//go:build !ignore_coverage
package main
```

测试文件本身不被覆盖率统计（`go test -cover` 默认跳过）。